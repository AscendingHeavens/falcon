package falcon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ascendingheavens/falcon/middleware"
	"github.com/stretchr/testify/assert"
)

func TestResponses_MiddlewareCanChangeThemOnTheWayOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	assert.NoError(t, os.WriteFile(path, []byte("file"), 0644))

	handlers := map[string]HandlerFunc{
		"a Response literal": func(c *Context) *Response { return &Response{Success: true, Message: "ok", Code: 200} },
		"c.JSON":             func(c *Context) *Response { return c.JSON(true, "ok", nil, 200) },
		"c.OK":               func(c *Context) *Response { return c.OK("ok", nil) },
		"c.ErrorJSON":        func(c *Context) *Response { return c.ErrorJSON("no", nil, 400) },
		"c.Error":            func(c *Context) *Response { return c.Error(NewHTTPError(400, "no")) },
		"c.String":           func(c *Context) *Response { return c.String(200, "text") },
		"c.HTML":             func(c *Context) *Response { return c.HTML(200, "<p>") },
		"c.Blob":             func(c *Context) *Response { return c.Blob(200, []byte("b"), "a/b") },
		"c.Stream":           func(c *Context) *Response { return c.Stream(200, "a/b", strings.NewReader("s")) },
		"c.File":             func(c *Context) *Response { return c.File(path) },
		"c.Redirect":         func(c *Context) *Response { return c.Redirect(302, "/x") },
		"c.NoContent":        func(c *Context) *Response { return c.NoContent() },
	}

	for name, handler := range handlers {
		t.Run(name, func(t *testing.T) {
			s := New()
			s.Use(func(next HandlerFunc) HandlerFunc {
				return func(c *Context) *Response {
					resp := next(c)
					// Too late under the old model for anything but a Response literal.
					c.Writer.Header().Set("X-After", "1")
					return resp
				}
			})
			s.GET("/", handler)

			assert.Equal(t, "1", do(s, "GET", "/").Header().Get("X-After"))
		})
	}
}

func TestResponses_MiddlewareCanReplaceThem(t *testing.T) {
	s := New()
	s.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) *Response {
			next(c)
			return c.ErrorJSON("replaced", nil, 503)
		}
	})
	s.GET("/", func(c *Context) *Response { return c.HTML(200, "<p>original</p>") })

	rec := do(s, "GET", "/")

	assert.Equal(t, 503, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.NotContains(t, rec.Body.String(), "original")
}

func TestResponses_HandlerCanTakeOverTheWriter(t *testing.T) {
	s := New()
	s.GET("/", func(c *Context) *Response {
		c.Writer.WriteHeader(201)
		_, _ = c.Writer.Write([]byte("mine"))
		c.Handled = true
		return &Response{Success: true, Message: "ignored", Code: 500}
	})

	rec := do(s, "GET", "/")

	assert.Equal(t, 201, rec.Code)
	assert.Equal(t, "mine", rec.Body.String())
}

func TestResponses_HeadOnAFileHasNoBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	assert.NoError(t, os.WriteFile(path, []byte("file"), 0644))
	s := New()
	s.GET("/", func(c *Context) *Response { return c.File(path) })

	rec := do(s, "HEAD", "/")

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "4", rec.Header().Get("Content-Length"))
	assert.Empty(t, rec.Body.String())
}

func TestResponses_RecoveryStillAnswersAPanic(t *testing.T) {
	s := New()
	s.Use(middleware.Recovery())
	s.GET("/", func(c *Context) *Response { panic("boom") })

	rec := do(s, "GET", "/")

	assert.Equal(t, 500, rec.Code)
	assert.Equal(t, "Internal Server Error", decode(t, rec).Message)
	assert.NotContains(t, rec.Body.String(), "boom")

	debug := New(WithDebug())
	debug.Use(middleware.Recovery())
	debug.GET("/", func(c *Context) *Response { panic("boom") })
	assert.Contains(t, do(debug, "GET", "/").Body.String(), "boom", "debug mode shows the panic")
}

// Everything a real handler does with bad input, from the request to the bytes sent.
func TestErrors_FullRequestFlow(t *testing.T) {
	type signup struct {
		Email string `json:"email" validate:"required,email"`
	}
	errTaken := errors.New("email taken")

	newServer := func(options ...Option) *Server {
		s := New(append(options, WithErrorMapper(func(err error) *Response {
			if errors.Is(err, errTaken) {
				return &Response{Message: "Already registered", Code: 409}
			}
			return nil
		}))...)
		s.POST("/signup", func(c *Context) *Response {
			var req signup
			if err := c.BindJSON(&req); err != nil {
				return c.Error(err)
			}
			if err := c.Validate(&req); err != nil {
				return c.Error(err)
			}
			switch req.Email {
			case "taken@example.com":
				return c.Error(fmt.Errorf("creating user: %w", errTaken))
			case "crash@example.com":
				return c.Error(errors.New("connecting to 10.0.0.5: refused"))
			}
			return c.Created("Signed up", req)
		})
		return s
	}

	post := func(s *Server, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/signup", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}

	tests := []struct {
		name string
		body string
		code int
		json string
	}{
		{"success", `{"email":"a@example.com"}`, 201, `{"success":true,"message":"Signed up","details":{"email":"a@example.com"},"code":201}`},
		{"malformed body", `{"email":`, 400, `{"success":false,"message":"Invalid JSON body","code":400}`},
		{"invalid field", `{"email":"nope"}`, 422, `{"success":false,"message":"Validation failed","details":[{"field":"email","rule":"email","message":"must be a valid email"}],"code":422}`},
		{"mapped domain error", `{"email":"taken@example.com"}`, 409, `{"success":false,"message":"Already registered","code":409}`},
		{"unknown error hides the cause", `{"email":"crash@example.com"}`, 500, `{"success":false,"message":"Internal Server Error","code":500}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := post(newServer(), tt.body)
			assert.Equal(t, tt.code, rec.Code)
			assert.JSONEq(t, tt.json, rec.Body.String())
		})
	}

	t.Run("debug mode shows the cause", func(t *testing.T) {
		rec := post(newServer(WithDebug()), `{"email":"crash@example.com"}`)
		assert.Equal(t, 500, rec.Code)
		assert.Contains(t, rec.Body.String(), "10.0.0.5")

		rec = post(newServer(WithDebug()), `{"email":`)
		assert.Equal(t, 400, rec.Code)
		assert.Contains(t, rec.Body.String(), "unexpected end")
	})

	t.Run("the body limit applies", func(t *testing.T) {
		rec := post(newServer(WithBodyLimit(5)), `{"email":"a@example.com"}`)
		assert.Equal(t, 413, rec.Code)
	})
}

func TestOptions(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		s := New()
		assert.False(t, s.settings.Debug)
		assert.Nil(t, s.settings.ErrorMapper)
		assert.Equal(t, int64(10<<20), s.settings.BodyLimit)
		assert.Equal(t, int64(32<<20), s.settings.MultipartMemory)
		assert.Equal(t, defaultTimeouts, s.timeouts)
	})

	t.Run("are applied", func(t *testing.T) {
		timeouts := Timeouts{ReadHeader: time.Second}
		s := New(WithDebug(), WithBodyLimit(1), WithMultipartMemory(2), WithTimeouts(timeouts))
		assert.True(t, s.settings.Debug)
		assert.Equal(t, int64(1), s.settings.BodyLimit)
		assert.Equal(t, int64(2), s.settings.MultipartMemory)

		srv := s.newHTTPServer(":0")
		assert.Equal(t, time.Second, srv.ReadHeaderTimeout)
		assert.Zero(t, srv.WriteTimeout, "a limit left at zero is turned off")
	})

	t.Run("nonsense sizes panic", func(t *testing.T) {
		assert.Panics(t, func() { WithBodyLimit(0) })
		assert.Panics(t, func() { WithMultipartMemory(-1) })
	})
}

// freeAddr returns a local address nothing is listening on.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	defer l.Close()
	return l.Addr().String()
}

// startServer runs s until the test ends and returns the error Start gave.
func startServer(t *testing.T, s *Server, addr string) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- s.Start(addr) }()

	for range 200 {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			conn.Close()
			return done
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("server didn't start")
	return nil
}

func TestStart_ReturnsAnErrorInsteadOfExiting(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	defer l.Close()

	assert.Error(t, New().Start(l.Addr().String()), "the port is taken")
}

func TestShutdown_WaitsForRequestsInProgress(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	s := New()
	s.GET("/slow", func(c *Context) *Response {
		close(started)
		<-release
		return c.OK("finished", nil)
	})

	addr := freeAddr(t)
	stopped := startServer(t, s, addr)

	body := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/slow")
		if err != nil {
			body <- err.Error()
			return
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		body <- string(data)
	}()
	<-started

	shutdown := make(chan error, 1)
	go func() { shutdown <- s.Shutdown(context.Background()) }()

	select {
	case <-shutdown:
		t.Fatal("Shutdown returned while a request was still running")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	assert.Contains(t, <-body, "finished", "the request in progress must complete")
	assert.NoError(t, <-shutdown)
	assert.NoError(t, <-stopped, "a graceful stop isn't an error")
}

func TestShutdown_HonoursItsContext(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	s := New()
	s.GET("/stuck", func(c *Context) *Response {
		close(started)
		<-release
		return c.NoContent()
	})

	addr := freeAddr(t)
	startServer(t, s, addr)
	go func() {
		if resp, err := http.Get("http://" + addr + "/stuck"); err == nil {
			resp.Body.Close()
		}
	}()
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	assert.ErrorIs(t, s.Shutdown(ctx), context.DeadlineExceeded)
}

func TestShutdown_BeforeStartStopsItFromListening(t *testing.T) {
	s := New()
	assert.NoError(t, s.Shutdown(context.Background()))
	assert.NoError(t, s.Start(freeAddr(t)))
}

// A stream outlasting the write timeout must finish. Everything else must still be limited.
func TestWriteTimeout_DoesNotCutOffStreams(t *testing.T) {
	const timeout = 50 * time.Millisecond

	s := New(WithTimeouts(Timeouts{Write: timeout}))
	s.GET("/stream", func(c *Context) *Response {
		time.Sleep(2 * timeout) // So the deadline set at the start of the request has passed
		return c.Stream(200, "text/plain", strings.NewReader("all of it"))
	})
	s.GET("/plain", func(c *Context) *Response {
		time.Sleep(2 * timeout)
		return c.OK("too late", nil)
	})

	addr := freeAddr(t)
	stopped := startServer(t, s, addr)
	defer func() {
		assert.NoError(t, s.Shutdown(context.Background()))
		assert.NoError(t, <-stopped)
	}()

	resp, err := http.Get("http://" + addr + "/stream")
	if assert.NoError(t, err) {
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		assert.NoError(t, err)
		assert.Equal(t, "all of it", string(data))
	}

	_, err = http.Get("http://" + addr + "/plain")
	assert.Error(t, err, "an ordinary response past the write timeout is cut off")
}

func TestServeHTTP_JSONFastPathAllocations(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts mean nothing under the race detector")
	}

	s := New()
	s.GET("/", func(c *Context) *Response {
		return &Response{Success: true, Message: "pong", Code: 200}
	})
	w := nopWriter{header: make(http.Header)}
	req := httptest.NewRequest("GET", "/", nil)
	do(s, "GET", "/") // fill the pools

	allocs := testing.AllocsPerRun(1000, func() { s.ServeHTTP(w, req) })

	assert.LessOrEqual(t, allocs, 1.0, "only the Response the handler itself created")
}
