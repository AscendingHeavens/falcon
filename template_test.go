package falcon

import (
	"bytes"
	"context"
	"embed"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ascendingheavens/falcon/middleware"
	"github.com/stretchr/testify/assert"
)

//go:embed testdata/views
var embeddedViews embed.FS

func viewsRenderer(t *testing.T) *TemplateRenderer {
	t.Helper()
	sub, err := fs.Sub(embeddedViews, "testdata/views")
	assert.NoError(t, err)
	renderer, err := NewTemplateRendererFS(sub, nil)
	assert.NoError(t, err)
	return renderer
}

// This is the test that didn't exist when templating was removed: the old version only
// worked in tests that built a Context by hand, and crashed on every real request.
func TestTemplates_ThroughServeHTTP(t *testing.T) {
	s := New()
	s.SetRenderer(viewsRenderer(t))
	s.GET("/users/:id", func(c *Context) *Response {
		return c.Render(200, "users/show.html", map[string]string{"Name": c.Param("id")})
	})

	rec := do(s, "GET", "/users/ada")

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "<nav>Falcon</nav>", "partial")
	assert.Contains(t, rec.Body.String(), "<main><h1>User ada</h1></main>", "page inside layout")
}

func TestTemplates_FromADirectory(t *testing.T) {
	renderer, err := NewTemplateRenderer("testdata/views", nil)
	assert.NoError(t, err)

	s := New()
	s.SetRenderer(renderer)
	s.GET("/", func(c *Context) *Response {
		return c.Render(200, "users/show.html", map[string]string{"Name": "dir"})
	})

	assert.Contains(t, do(s, "GET", "/").Body.String(), "<h1>User dir</h1>")
}

func TestTemplates_OverARealServer(t *testing.T) {
	s := New()
	s.Use(middleware.Logger())
	s.Use(middleware.Recovery())
	s.SetRenderer(viewsRenderer(t))
	s.GET("/page", func(c *Context) *Response {
		return c.Render(200, "users/show.html", map[string]string{"Name": "real"})
	})
	s.GET("/missing", func(c *Context) *Response {
		return c.Render(200, "nope.html", nil)
	})

	addr := freeAddr(t)
	stopped := startServer(t, s, addr)
	defer func() {
		assert.NoError(t, s.Shutdown(context.Background()))
		assert.NoError(t, <-stopped)
	}()

	resp, err := http.Get("http://" + addr + "/page")
	if assert.NoError(t, err) {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		assert.Equal(t, 200, resp.StatusCode)
		assert.Contains(t, string(body), "<h1>User real</h1>")
	}

	head, err := http.Head("http://" + addr + "/page")
	if assert.NoError(t, err) {
		_ = head.Body.Close()
		assert.Equal(t, 200, head.StatusCode)
		assert.Equal(t, "text/html; charset=utf-8", head.Header.Get("Content-Type"))
	}

	failed, err := http.Get("http://" + addr + "/missing")
	if assert.NoError(t, err) {
		defer func() { _ = failed.Body.Close() }()
		body, _ := io.ReadAll(failed.Body)
		assert.Equal(t, 500, failed.StatusCode)
		assert.JSONEq(t, `{"success":false,"message":"Internal Server Error","code":500}`, string(body))
	}
}

func TestTemplates_WithDebugTheOptionControlsReloading(t *testing.T) {
	newApp := func(fsys fstest.MapFS, options ...Option) *Server {
		renderer, err := NewTemplateRendererFS(fsys, nil)
		assert.NoError(t, err)
		s := New(options...)
		s.SetRenderer(renderer)
		s.GET("/", func(c *Context) *Response { return c.Render(200, "a.html", nil) })
		return s
	}

	fsys := fstest.MapFS{"a.html": &fstest.MapFile{Data: []byte("before")}}
	production := newApp(fsys)
	development := newApp(fsys, WithDebug())

	fsys["a.html"] = &fstest.MapFile{Data: []byte("after")}

	assert.Equal(t, "before", do(production, "GET", "/").Body.String())
	assert.Equal(t, "after", do(development, "GET", "/").Body.String())
}

func TestTemplates_WithoutARendererIsA500NotACrash(t *testing.T) {
	s := New()
	s.GET("/", func(c *Context) *Response { return c.Render(200, "a.html", nil) })

	rec := do(s, "GET", "/")

	assert.Equal(t, 500, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

func TestTemplates_SetRendererAfterServingPanics(t *testing.T) {
	s := New()
	s.GET("/", ok)
	do(s, "GET", "/")

	assert.Panics(t, func() { s.SetRenderer(viewsRenderer(t)) })
}

func TestTemplates_MiddlewareCanChangeThePage(t *testing.T) {
	s := New()
	s.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) *Response {
			resp := next(c)
			c.Writer.Header().Set("X-After", "1")
			if c.Request.URL.Path == "/replaced" {
				return c.ErrorJSON("replaced", nil, 503)
			}
			return resp
		}
	})
	s.SetRenderer(viewsRenderer(t))
	page := func(c *Context) *Response {
		return c.Render(200, "users/show.html", map[string]string{"Name": "page"})
	}
	s.GET("/page", page)
	s.GET("/replaced", page)

	assert.Equal(t, "1", do(s, "GET", "/page").Header().Get("X-After"))

	replaced := do(s, "GET", "/replaced")
	assert.Equal(t, 503, replaced.Code)
	assert.NotContains(t, replaced.Body.String(), "User page")

	// The abandoned page's buffer must not corrupt later ones.
	assert.Contains(t, do(s, "GET", "/page").Body.String(), "<h1>User page</h1>")
}

var csrfField = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// csrfApp is a form page and the handler it posts to, as a server-rendered app would have.
func csrfApp(t *testing.T) *Server {
	t.Helper()
	s := New()
	s.Use(middleware.CSRF())
	s.SetRenderer(viewsRenderer(t))
	s.GET("/contact", func(c *Context) *Response {
		return c.Render(200, "contact.html", map[string]any{"CSRFToken": c.Get("csrf_token")})
	})
	s.POST("/contact", func(c *Context) *Response {
		var form struct {
			Message string `form:"message"`
		}
		if err := c.Bind(&form); err != nil {
			return c.Error(err)
		}
		return c.OK(form.Message, nil)
	})
	s.POST("/api", func(c *Context) *Response {
		var req struct {
			Message string `json:"message"`
		}
		if err := c.BindJSON(&req); err != nil {
			return c.Error(err)
		}
		return c.OK(req.Message, nil)
	})
	return s
}

// loadForm does what a browser does first, and returns the cookie and the token in the page.
func loadForm(t *testing.T, s *Server) (*http.Cookie, string) {
	t.Helper()
	rec := do(s, "GET", "/contact")
	assert.Equal(t, 200, rec.Code)

	match := csrfField.FindStringSubmatch(rec.Body.String())
	if !assert.Len(t, match, 2, "the rendered page must contain the token") {
		t.FailNow()
	}
	return rec.Result().Cookies()[0], match[1]
}

func TestCSRFForm_FullBrowserFlow(t *testing.T) {
	s := csrfApp(t)
	cookie, token := loadForm(t, s)

	submit := func(target, body, contentType string, withCookie bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", target, strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		if withCookie {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}
	const urlEncoded = "application/x-www-form-urlencoded"
	valid := url.Values{"csrf_token": {token}, "message": {"hello"}}.Encode()

	t.Run("a form with the token works, and the handler can still read it", func(t *testing.T) {
		rec := submit("/contact", valid, urlEncoded, true)
		assert.Equal(t, 200, rec.Code)
		assert.Equal(t, "hello", decode(t, rec).Message)
	})
	t.Run("a forged form from another site has the cookie but no token", func(t *testing.T) {
		assert.Equal(t, 403, submit("/contact", "message=hacked", urlEncoded, true).Code)
	})
	t.Run("a wrong token", func(t *testing.T) {
		body := url.Values{"csrf_token": {"guess"}, "message": {"hacked"}}.Encode()
		assert.Equal(t, 403, submit("/contact", body, urlEncoded, true).Code)
	})
	t.Run("the cookie's own value as the token", func(t *testing.T) {
		body := url.Values{"csrf_token": {cookie.Value}}.Encode()
		assert.Equal(t, 403, submit("/contact", body, urlEncoded, true).Code)
	})
	t.Run("a valid token without the cookie", func(t *testing.T) {
		assert.Equal(t, 403, submit("/contact", valid, urlEncoded, false).Code)
	})
	t.Run("a token in the URL doesn't count", func(t *testing.T) {
		target := "/contact?csrf_token=" + url.QueryEscape(token)
		assert.Equal(t, 403, submit(target, "message=hello", urlEncoded, true).Code)
	})

	t.Run("a multipart form", func(t *testing.T) {
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		assert.NoError(t, w.WriteField("csrf_token", token))
		assert.NoError(t, w.WriteField("message", "from multipart"))
		assert.NoError(t, w.Close())

		rec := submit("/contact", body.String(), w.FormDataContentType(), true)
		assert.Equal(t, 200, rec.Code)
		assert.Equal(t, "from multipart", decode(t, rec).Message)
	})
}

func TestCSRFForm_JSONRequestsStillUseTheHeader(t *testing.T) {
	s := csrfApp(t)
	cookie, token := loadForm(t, s)

	post := func(body string, header string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		if header != "" {
			req.Header.Set("X-CSRF-Token", header)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}

	t.Run("the header works and the body is untouched", func(t *testing.T) {
		rec := post(`{"message":"hi"}`, token)
		assert.Equal(t, 200, rec.Code)
		assert.Equal(t, "hi", decode(t, rec).Message)
	})
	t.Run("no header is rejected", func(t *testing.T) {
		assert.Equal(t, 403, post(`{"message":"hi"}`, "").Code)
	})
	t.Run("a token inside JSON doesn't count", func(t *testing.T) {
		assert.Equal(t, 403, post(`{"csrf_token":"`+token+`"}`, "").Code)
	})
}

func TestCSRFForm_BodyLimitStillApplies(t *testing.T) {
	s := New(WithBodyLimit(200))
	s.Use(middleware.CSRF())
	s.GET("/token", func(c *Context) *Response { return c.OK(c.Get("csrf_token").(string), nil) })
	s.POST("/", ok)

	page := do(s, "GET", "/token")
	cookie := page.Result().Cookies()[0]
	token := decode(t, page).Message

	post := func(size int) int {
		form := url.Values{"csrf_token": {token}, "message": {strings.Repeat("x", size)}}.Encode()
		req := httptest.NewRequest("POST", "/", strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec.Code
	}

	assert.Equal(t, 200, post(10), "the token is valid")
	assert.Equal(t, 403, post(1000), "the same token, in a form too large to be read")
}
