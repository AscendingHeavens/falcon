package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newResponseContext(method string) (*Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	return &Context{Writer: rec, Request: httptest.NewRequest(method, "/", nil)}, rec
}

// assertNothingWritten checks that a helper only described the response.
func assertNothingWritten(t *testing.T, c *Context, rec *httptest.ResponseRecorder) {
	t.Helper()
	assert.False(t, c.Handled)
	assert.False(t, rec.Flushed)
	assert.Empty(t, rec.Body.String())
	assert.Empty(t, rec.Header())
}

func TestRawHelpers(t *testing.T) {
	tests := []struct {
		name        string
		build       func(c *Context) *Response
		code        int
		contentType string
		body        string
	}{
		{"String", func(c *Context) *Response { return c.String(201, "Hello") }, 201, "text/plain", "Hello"},
		{"HTML", func(c *Context) *Response { return c.HTML(200, "<h1>Hi</h1>") }, 200, "text/html", "<h1>Hi</h1>"},
		{"Blob", func(c *Context) *Response { return c.Blob(200, []byte{1, 2, 3}, "image/png") }, 200, "image/png", "\x01\x02\x03"},
		{"Stream", func(c *Context) *Response { return c.Stream(202, "text/csv", strings.NewReader("a,b")) }, 202, "text/csv", "a,b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec := newResponseContext(http.MethodGet)

			resp := tt.build(c)
			assertNothingWritten(t, c, rec)
			assert.True(t, resp.IsRaw())
			assert.Equal(t, tt.contentType, resp.ContentType())
			assert.Equal(t, tt.code, resp.Code)
			assert.True(t, resp.Success)

			c.WriteResponse(resp)
			assert.Equal(t, tt.code, rec.Code)
			assert.Equal(t, tt.contentType, rec.Header().Get("Content-Type"))
			assert.Equal(t, tt.body, rec.Body.String())
			assert.True(t, c.Handled)
		})
	}

	t.Run("an error status isn't a success", func(t *testing.T) {
		c, _ := newResponseContext(http.MethodGet)
		assert.False(t, c.HTML(500, "<h1>Oops</h1>").Success)
	})
}

func TestEnvelopeHelpers(t *testing.T) {
	details := map[string]string{"id": "1"}

	tests := []struct {
		name string
		resp func(c *Context) *Response
		want Response
	}{
		{"OK", func(c *Context) *Response { return c.OK("found", details) }, Response{Success: true, Message: "found", Details: details, Code: 200}},
		{"Created", func(c *Context) *Response { return c.Created("made", details) }, Response{Success: true, Message: "made", Details: details, Code: 201}},
		{"JSON", func(c *Context) *Response { return c.JSON(true, "ok", nil, 202) }, Response{Success: true, Message: "ok", Code: 202}},
		{"ErrorJSON", func(c *Context) *Response { return c.ErrorJSON("bad", details, 400) }, Response{Message: "bad", Details: details, Code: 400}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec := newResponseContext(http.MethodGet)

			resp := tt.resp(c)
			assertNothingWritten(t, c, rec)
			assert.Equal(t, &tt.want, resp)
			assert.False(t, resp.IsRaw())
			assert.Empty(t, resp.ContentType())

			c.WriteResponse(resp)
			assert.Equal(t, tt.want.Code, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		})
	}
}

func TestNoContent(t *testing.T) {
	c, rec := newResponseContext(http.MethodGet)

	resp := c.NoContent()
	assert.True(t, resp.IsRaw())
	c.WriteResponse(resp)

	assert.Equal(t, 204, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Empty(t, rec.Header().Get("Content-Type"))
}

func TestRedirect(t *testing.T) {
	c, rec := newResponseContext(http.MethodGet)

	resp := c.Redirect(302, "/login")
	assertNothingWritten(t, c, rec)
	c.WriteResponse(resp)

	assert.Equal(t, 302, rec.Code)
	assert.Equal(t, "/login", rec.Header().Get("Location"))
}

func TestRedirect_ReplacedByMiddlewareLeavesNoLocation(t *testing.T) {
	c, rec := newResponseContext(http.MethodGet)

	c.Redirect(302, "/login")
	c.WriteResponse(c.ErrorJSON("forbidden", nil, 403))

	assert.Equal(t, 403, rec.Code)
	assert.Empty(t, rec.Header().Get("Location"))
}

func TestFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.txt")
	content := []byte("hello file")
	assert.NoError(t, os.WriteFile(path, content, 0644))

	t.Run("serves an existing file", func(t *testing.T) {
		c, rec := newResponseContext(http.MethodGet)

		resp := c.File(path)
		assertNothingWritten(t, c, rec)
		assert.Equal(t, 200, resp.Code)
		c.WriteResponse(resp)

		assert.Equal(t, 200, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")
		assert.NotEmpty(t, rec.Header().Get("Last-Modified"))
		assert.Equal(t, content, rec.Body.Bytes())
	})

	t.Run("supports range requests", func(t *testing.T) {
		c, rec := newResponseContext(http.MethodGet)
		c.Request.Header.Set("Range", "bytes=6-9")

		c.WriteResponse(c.File(path))

		assert.Equal(t, http.StatusPartialContent, rec.Code)
		assert.Equal(t, "file", rec.Body.String())
	})

	t.Run("supports caching", func(t *testing.T) {
		first, firstRec := newResponseContext(http.MethodGet)
		first.WriteResponse(first.File(path))

		c, rec := newResponseContext(http.MethodGet)
		c.Request.Header.Set("If-Modified-Since", firstRec.Header().Get("Last-Modified"))
		c.WriteResponse(c.File(path))

		assert.Equal(t, http.StatusNotModified, rec.Code)
		assert.Empty(t, rec.Body.String())
	})

	t.Run("a missing file is a 404 envelope without the path", func(t *testing.T) {
		c, rec := newResponseContext(http.MethodGet)

		resp := c.File("/secret/nonexistent.file")
		assert.False(t, resp.IsRaw())
		assert.Equal(t, 404, resp.Code)
		assert.Equal(t, "File not found", resp.Message)
		c.WriteResponse(resp)

		assert.Equal(t, 404, rec.Code)
		assert.NotContains(t, rec.Body.String(), "secret")
	})

	t.Run("a directory is a 404", func(t *testing.T) {
		c, _ := newResponseContext(http.MethodGet)
		assert.Equal(t, 404, c.File(t.TempDir()).Code)
	})

	t.Run("a file deleted before writing is a 404 envelope", func(t *testing.T) {
		gone := filepath.Join(t.TempDir(), "gone.txt")
		assert.NoError(t, os.WriteFile(gone, content, 0644))
		c, rec := newResponseContext(http.MethodGet)

		resp := c.File(gone)
		assert.NoError(t, os.Remove(gone))
		c.WriteResponse(resp)

		assert.Equal(t, 404, rec.Code)
		assert.Contains(t, rec.Body.String(), "File not found")
	})
}

type trackedReader struct {
	io.Reader
	closed int
}

func (r *trackedReader) Close() error {
	r.closed++
	return nil
}

func TestStream(t *testing.T) {
	t.Run("closes the reader after sending", func(t *testing.T) {
		c, rec := newResponseContext(http.MethodGet)
		r := &trackedReader{Reader: strings.NewReader("data")}

		resp := c.Stream(200, "text/plain", r)
		assert.Zero(t, r.closed, "not closed until it has been sent")
		c.WriteResponse(resp)

		assert.Equal(t, "data", rec.Body.String())
		assert.Equal(t, 1, r.closed)
	})

	t.Run("closes the reader when middleware replaces the response", func(t *testing.T) {
		c, rec := newResponseContext(http.MethodGet)
		r := &trackedReader{Reader: strings.NewReader("data")}

		c.Stream(200, "text/plain", r)
		c.WriteResponse(c.ErrorJSON("denied", nil, 403))

		assert.Equal(t, 403, rec.Code)
		assert.NotContains(t, rec.Body.String(), "data")
		assert.Equal(t, 1, r.closed)
	})

	t.Run("closes the first reader when a second replaces it", func(t *testing.T) {
		c, _ := newResponseContext(http.MethodGet)
		first := &trackedReader{Reader: strings.NewReader("first")}
		second := &trackedReader{Reader: strings.NewReader("second")}

		c.Stream(200, "text/plain", first)
		c.WriteResponse(c.Stream(200, "text/plain", second))

		assert.Equal(t, 1, first.closed)
		assert.Equal(t, 1, second.closed)
	})

	t.Run("doesn't hold the body in memory", func(t *testing.T) {
		c, rec := newResponseContext(http.MethodGet)
		c.WriteResponse(c.Stream(200, "text/plain", io.LimitReader(strings.NewReader(strings.Repeat("x", 1<<20)), 1<<20)))
		assert.Equal(t, 1<<20, rec.Body.Len())
	})
}

func TestWriteResponse(t *testing.T) {
	t.Run("writes nothing if the handler took over the writer", func(t *testing.T) {
		c, rec := newResponseContext(http.MethodGet)
		c.Handled = true

		c.WriteResponse(c.OK("ignored", nil))

		assert.Empty(t, rec.Body.String())
		assert.Empty(t, rec.Header())
	})

	t.Run("only writes once", func(t *testing.T) {
		c, rec := newResponseContext(http.MethodGet)

		c.WriteResponse(c.OK("first", nil))
		c.WriteResponse(c.OK("second", nil))

		assert.Contains(t, rec.Body.String(), "first")
		assert.NotContains(t, rec.Body.String(), "second")
	})

	t.Run("a nil response is a 500", func(t *testing.T) {
		c, rec := newResponseContext(http.MethodGet)
		c.WriteResponse(nil)
		assert.Equal(t, 500, rec.Code)
		assert.JSONEq(t, `{"success":false,"message":"Internal Server Error","code":500}`, rec.Body.String())
	})

	t.Run("a nil response is fine after taking over the writer", func(t *testing.T) {
		c, rec := newResponseContext(http.MethodGet)
		c.Handled = true
		c.WriteResponse(nil)
		assert.Empty(t, rec.Body.String())
	})

	t.Run("a hand-built response is always the envelope", func(t *testing.T) {
		c, rec := newResponseContext(http.MethodGet)
		c.WriteResponse(&Response{Success: true, Message: "hi", Code: 200})
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		assert.JSONEq(t, `{"success":true,"message":"hi","code":200}`, rec.Body.String())
	})

	for _, code := range []int{0, -1, 99, 1000} {
		t.Run("an invalid status code is a 500, not a panic", func(t *testing.T) {
			c, rec := newResponseContext(http.MethodGet)
			c.WriteResponse(&Response{Success: true, Message: "forgot the code", Code: code})

			assert.Equal(t, 500, rec.Code)
			assert.JSONEq(t, `{"success":false,"message":"Internal Server Error","code":500}`, rec.Body.String())
		})
	}

	t.Run("a status code that forbids a body doesn't get one", func(t *testing.T) {
		for _, code := range []int{204, 304} {
			c, rec := newResponseContext(http.MethodGet)
			c.WriteResponse(&Response{Success: true, Message: "no body", Code: code})
			assert.Equal(t, code, rec.Code)
			assert.Empty(t, rec.Body.String())
		}
	})

	t.Run("details that can't be encoded are a clean 500", func(t *testing.T) {
		c, rec := newResponseContext(http.MethodGet)
		c.WriteResponse(&Response{Success: true, Message: "ok", Details: make(chan int), Code: 200})

		assert.Equal(t, 500, rec.Code)
		assert.JSONEq(t, `{"success":false,"message":"Internal Server Error","code":500}`, rec.Body.String())
	})

	t.Run("a failed encode doesn't use the error mapper", func(t *testing.T) {
		c, rec := newResponseContext(http.MethodGet)
		c.settings = &Settings{ErrorMapper: func(error) *Response { return &Response{Details: make(chan int)} }}

		c.WriteResponse(&Response{Details: make(chan int), Code: 200})

		assert.Equal(t, 500, rec.Code)
	})

	t.Run("a large response doesn't leak into the next one", func(t *testing.T) {
		big, _ := newResponseContext(http.MethodGet)
		big.WriteResponse(big.OK(strings.Repeat("x", 1<<17), nil))

		for range 10 {
			c, rec := newResponseContext(http.MethodGet)
			c.WriteResponse(c.OK("small", nil))
			assert.Equal(t, `{"success":true,"message":"small","code":200}`+"\n", rec.Body.String())
		}
	})
}

// The fast path must send exactly what encoding/json would. If this drifts,
// frontends see different bytes depending on which path a response took.
func TestEnvelope_MatchesEncodingJSONExactly(t *testing.T) {
	messages := []string{
		"",
		"plain ascii",
		`quote " and \ backslash`,
		"<script>alert('x')</script> & more",
		"new\nline\ttab\rreturn",
		"control \x00\x01\x1f\x7f",
		"line sep   and para sep  ",
		"unicode: नमस्ते 日本語 🦅",
		"invalid utf8: \xff\xfe",
		"/slashes/are/fine",
	}
	details := []any{
		nil,
		"a string",
		`needs "escaping" <here>`,
		0,
		42,
		3.14,
		true,
		false,
		[]int{},
		[]int{1, 2},
		[]int(nil),
		map[string]any{},
		map[string]any{"b": 1, "a": []string{"<x>"}},
		struct {
			ID   int    `json:"id"`
			Name string `json:"name,omitempty"`
		}{ID: 1},
		(*int)(nil),
		json.RawMessage(`{"raw":true}`),
	}

	for _, message := range messages {
		for _, detail := range details {
			for _, success := range []bool{true, false} {
				resp := &Response{Success: success, Message: message, Details: detail, Code: 200}

				var want bytes.Buffer
				assert.NoError(t, json.NewEncoder(&want).Encode(resp))

				c, rec := newResponseContext(http.MethodGet)
				c.WriteResponse(resp)

				assert.Equal(t, want.String(), rec.Body.String())
			}
		}
	}
}

func TestEnvelope_FloatsThatCantBeEncoded(t *testing.T) {
	c, rec := newResponseContext(http.MethodGet)
	c.WriteResponse(&Response{Message: "nan", Details: math.NaN(), Code: 200})
	assert.Equal(t, 500, rec.Code)
}

func TestErrorResponse(t *testing.T) {
	cause := errors.New("db password is hunter2")
	secret := "hunter2"

	tests := []struct {
		name        string
		err         error
		wantCode    int
		wantMessage string
		wantDetails any
	}{
		{"an HTTPError", NewHTTPError(403, "You can't edit this"), 403, "You can't edit this", nil},
		{"an empty message uses the standard text", NewHTTPError(404, ""), 404, "Not Found", nil},
		{"details are sent", &HTTPError{Code: 409, Message: "Taken", Details: "try another"}, 409, "Taken", "try another"},
		{"the cause of a 4xx isn't sent", &HTTPError{Code: 400, Message: "Bad", Err: cause}, 400, "Bad", nil},
		{"the cause of a 5xx isn't sent", &HTTPError{Code: 503, Message: "Down", Err: cause}, 503, "Down", nil},
		{"a wrapped HTTPError still counts", fmtWrap(NewHTTPError(418, "Teapot")), 418, "Teapot", nil},
		{"an unknown error is a generic 500", cause, 500, "Internal Server Error", nil},
		{"a nil error is a generic 500", nil, 500, "Internal Server Error", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec := newResponseContext(http.MethodGet)

			resp := c.Error(tt.err)

			assert.False(t, resp.Success)
			assert.Equal(t, tt.wantCode, resp.Code)
			assert.Equal(t, tt.wantMessage, resp.Message)
			assert.Equal(t, tt.wantDetails, resp.Details)

			c.WriteResponse(resp)
			assert.NotContains(t, rec.Body.String(), secret)
		})
	}
}

// fmtWrap wraps an error the way application code does on its way up the stack.
func fmtWrap(err error) error {
	return &wrapError{err}
}

type wrapError struct{ err error }

func (w *wrapError) Error() string { return "while handling: " + w.err.Error() }
func (w *wrapError) Unwrap() error { return w.err }

func TestErrorResponse_Debug(t *testing.T) {
	cause := errors.New("db is down")
	newDebug := func() *Context {
		c, _ := newResponseContext(http.MethodGet)
		c.settings = &Settings{Debug: true}
		return c
	}

	assert.Equal(t, "db is down", newDebug().Error(cause).Details)
	assert.Equal(t, "db is down", newDebug().Error(&HTTPError{Code: 400, Err: cause}).Details)
	assert.Equal(t, "mine", newDebug().Error(&HTTPError{Code: 400, Details: "mine", Err: cause}).Details,
		"details set on purpose aren't replaced")
}

func TestErrorResponse_Mapper(t *testing.T) {
	errNotFound := errors.New("not found")
	var seen []error
	settings := &Settings{ErrorMapper: func(err error) *Response {
		seen = append(seen, err)
		if errors.Is(err, errNotFound) {
			return &Response{Message: "No such user", Code: 404}
		}
		return nil
	}}
	c, _ := newResponseContext(http.MethodGet)
	c.settings = settings

	t.Run("maps the errors it knows", func(t *testing.T) {
		resp := c.Error(fmtWrap(errNotFound))
		assert.Equal(t, 404, resp.Code)
		assert.Equal(t, "No such user", resp.Message)
	})
	t.Run("leaves the rest to the default handling", func(t *testing.T) {
		assert.Equal(t, 500, c.Error(errors.New("other")).Code)
		assert.Equal(t, 403, c.Error(NewHTTPError(403, "no")).Code)
	})
	t.Run("runs before anything else", func(t *testing.T) {
		seen = nil
		c.Error(NewHTTPError(403, "no"))
		c.Error(&ValidationError{})
		assert.Len(t, seen, 2)
	})
}

func TestHTTPError(t *testing.T) {
	cause := errors.New("cause")
	err := &HTTPError{Code: 400, Message: "Bad", Err: cause}

	assert.ErrorIs(t, err, cause)
	assert.Equal(t, "400 Bad: cause", err.Error())
	assert.Equal(t, "404 Missing", NewHTTPError(404, "Missing").Error())
}
