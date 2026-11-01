package falcon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ascendingheavens/falcon/middleware"
	"github.com/ascendingheavens/falcon/server"
	"github.com/stretchr/testify/assert"
)

func ok(c *Context) *Response {
	return &Response{Success: true, Message: "ok", Code: 200}
}

func do(s *Server, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

// tracer returns a middleware that records its name when it runs.
func tracer(name string, trace *[]string) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) *Response {
			*trace = append(*trace, name)
			return next(c)
		}
	}
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) Response {
	t.Helper()
	var resp Response
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp), "body should be the envelope: %q", rec.Body.String())
	return resp
}

func TestFallback_NotFoundIsAnEnvelope(t *testing.T) {
	s := New()
	s.GET("/users", ok)

	rec := do(s, "GET", "/nope")

	assert.Equal(t, 404, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	resp := decode(t, rec)
	assert.False(t, resp.Success)
	assert.Equal(t, "Not Found", resp.Message)
	assert.Equal(t, 404, resp.Code)
}

func TestFallback_WrongMethodIs405WithAllow(t *testing.T) {
	s := New()
	s.GET("/users/:id", ok)
	s.DELETE("/users/:id", ok)

	rec := do(s, "POST", "/users/1")

	assert.Equal(t, 405, rec.Code)
	assert.Equal(t, "DELETE, GET, HEAD, OPTIONS", rec.Header().Get("Allow"))
	resp := decode(t, rec)
	assert.False(t, resp.Success)
	assert.Equal(t, "Method Not Allowed", resp.Message)
}

func TestFallback_TrailingSlashRedirects(t *testing.T) {
	s := New()
	s.GET("/users", ok)
	s.POST("/users", ok)
	s.GET("/dir/", ok)
	s.GET("/items/:id", ok)
	s.GET("/static/*filepath", ok)

	tests := []struct {
		name     string
		method   string
		target   string
		code     int
		location string
	}{
		{"remove slash on GET", "GET", "/users/", 301, "/users"},
		{"add slash on GET", "GET", "/dir", 301, "/dir/"},
		{"HEAD behaves like GET", "HEAD", "/users/", 301, "/users"},
		{"POST keeps its method", "POST", "/users/", 308, "/users"},
		{"query string is kept", "GET", "/users/?a=1&b=2", 301, "/users?a=1&b=2"},
		{"param route", "GET", "/items/5/", 301, "/items/5"},
		{"catch-all needs its slash", "GET", "/static", 301, "/static/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(s, tt.method, tt.target)
			assert.Equal(t, tt.code, rec.Code)
			assert.Equal(t, tt.location, rec.Header().Get("Location"))
		})
	}

	t.Run("no redirect when the other form has no route for this method", func(t *testing.T) {
		assert.Equal(t, 404, do(s, "DELETE", "/users/").Code)
	})
	t.Run("no redirect from the root", func(t *testing.T) {
		assert.Equal(t, 404, do(s, "GET", "/").Code)
	})
}

func TestFallback_TrailingSlashRedirectStaysOnThisSite(t *testing.T) {
	s := New()
	s.GET("/:page", ok)

	// Browsers treat a backslash as a slash, so an unescaped "/\evil.com" means "//evil.com".
	rec := do(s, "GET", `/%5Cevil.com/`)

	assert.Equal(t, 301, rec.Code)
	assert.Equal(t, "/%5Cevil.com", rec.Header().Get("Location"))
}

func TestFallback_HeadUsesGetWithoutBody(t *testing.T) {
	var params string
	s := New()
	s.GET("/users/:id", func(c *Context) *Response {
		params = c.Param("id")
		c.Writer.Header().Set("X-From-Get", "1")
		return ok(c)
	})

	rec := do(s, "HEAD", "/users/7")

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "7", params)
	assert.Equal(t, "1", rec.Header().Get("X-From-Get"))
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Empty(t, rec.Body.String())
}

func TestFallback_HeadOnErrorsHasNoBody(t *testing.T) {
	s := New()
	s.POST("/users", ok)

	notFound := do(s, "HEAD", "/nope")
	assert.Equal(t, 404, notFound.Code)
	assert.Empty(t, notFound.Body.String())

	wrongMethod := do(s, "HEAD", "/users")
	assert.Equal(t, 405, wrongMethod.Code)
	assert.Empty(t, wrongMethod.Body.String())
}

func TestFallback_ExplicitHeadRouteWins(t *testing.T) {
	s := New()
	s.GET("/x", ok)
	s.Handle("HEAD", "/x", func(c *Context) *Response {
		c.Writer.Header().Set("X-Explicit", "1")
		return ok(c)
	})

	assert.Equal(t, "1", do(s, "HEAD", "/x").Header().Get("X-Explicit"))
}

func TestFallback_Options(t *testing.T) {
	s := New()
	s.GET("/users", ok)
	s.Handle("OPTIONS", "/custom", func(c *Context) *Response {
		return &Response{Success: true, Message: "mine", Code: 200}
	})

	t.Run("known path gets 204 and Allow", func(t *testing.T) {
		rec := do(s, "OPTIONS", "/users")
		assert.Equal(t, 204, rec.Code)
		assert.Equal(t, "GET, HEAD, OPTIONS", rec.Header().Get("Allow"))
		assert.Empty(t, rec.Body.String())
	})
	t.Run("unknown path is a 404", func(t *testing.T) {
		assert.Equal(t, 404, do(s, "OPTIONS", "/nope").Code)
	})
	t.Run("a registered OPTIONS route wins", func(t *testing.T) {
		rec := do(s, "OPTIONS", "/custom")
		assert.Equal(t, 200, rec.Code)
		assert.Equal(t, "mine", decode(t, rec).Message)
	})
}

func TestFallback_GlobalCORSAnswersPreflight(t *testing.T) {
	s := New()
	s.Use(middleware.CORS())
	s.POST("/users", ok)

	req := httptest.NewRequest("OPTIONS", "/users", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	assert.Equal(t, 204, rec.Code)
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "POST")
}

func TestFallback_GlobalMiddlewareSeesServerResponses(t *testing.T) {
	var codes []int
	s := New()
	s.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) *Response {
			resp := next(c)
			codes = append(codes, resp.Code)
			return resp
		}
	})
	s.GET("/users", ok)

	do(s, "GET", "/nope")
	do(s, "POST", "/users")
	do(s, "GET", "/users/")
	do(s, "OPTIONS", "/users")

	assert.Equal(t, []int{404, 405, 301, 204}, codes)
}

func TestChains_UseAppliesToRoutesRegisteredBeforeIt(t *testing.T) {
	var trace []string
	s := New()
	s.GET("/early", ok)
	group := s.Group("/api")
	group.GET("/early", ok)

	s.Use(tracer("global", &trace))
	group.Use(tracer("group", &trace))

	do(s, "GET", "/early")
	assert.Equal(t, []string{"global"}, trace)

	trace = nil
	do(s, "GET", "/api/early")
	assert.Equal(t, []string{"global", "group"}, trace)
}

func TestChains_OrderIsGlobalConditionalGroupHandler(t *testing.T) {
	var trace []string
	s := New()
	group := s.Group("/api")

	// Registered in the opposite order on purpose.
	group.Use(tracer("group 1", &trace))
	group.Use(tracer("group 2", &trace))
	s.UseIf("/api/*", tracer("if 1", &trace))
	s.UseIf("/api/*", tracer("if 2", &trace))
	s.Use(tracer("global 1", &trace))
	s.Use(tracer("global 2", &trace))

	group.GET("/x", func(c *Context) *Response {
		trace = append(trace, "handler")
		return ok(c)
	})

	do(s, "GET", "/api/x")
	assert.Equal(t, []string{"global 1", "global 2", "if 1", "if 2", "group 1", "group 2", "handler"}, trace)
}

func TestChains_UseIfOnlyWrapsMatchingRoutes(t *testing.T) {
	var trace []string
	s := New()
	s.UseIf("/api/*", tracer("if", &trace))
	s.GET("/api/users/:id", ok)
	s.GET("/public", ok)

	do(s, "GET", "/public")
	assert.Empty(t, trace)

	do(s, "GET", "/api/users/1")
	assert.Equal(t, []string{"if"}, trace)
}

func TestChains_RecoveryCoversConditionalMiddleware(t *testing.T) {
	s := New()
	s.Use(middleware.Recovery())
	s.UseIf("/api/*", func(next HandlerFunc) HandlerFunc {
		return func(c *Context) *Response { panic("auth exploded") }
	})
	s.GET("/api/x", ok)

	assert.Equal(t, 500, do(s, "GET", "/api/x").Code)
}

func TestChains_RegisteringAfterServingPanics(t *testing.T) {
	s := New()
	group := s.Group("/api")
	s.GET("/x", ok)
	do(s, "GET", "/x")

	mw := func(next HandlerFunc) HandlerFunc { return next }
	for name, register := range map[string]func(){
		"Use":          func() { s.Use(mw) },
		"UseIf":        func() { s.UseIf("/x", mw) },
		"Handle":       func() { s.GET("/y", ok) },
		"Group.Use":    func() { group.Use(mw) },
		"Group.Handle": func() { group.GET("/y", ok) },
	} {
		t.Run(name, func(t *testing.T) {
			assert.PanicsWithValue(t,
				"falcon: "+name+" called after the server started serving; register everything before the first request",
				register)
		})
	}
}

func TestChains_ConflictPanicsAtRegistration(t *testing.T) {
	s := New()
	s.GET("/users/:id", ok)
	assert.Panics(t, func() { s.GET("/users/:name", ok) })

	group := s.Group("/users")
	assert.Panics(t, func() { group.GET("/:name", ok) })
}

func TestChains_BrokenMiddlewareFailsEveryRequest(t *testing.T) {
	s := New()
	s.Use(func(next HandlerFunc) HandlerFunc { panic("bad config") })
	s.GET("/x", ok)

	// It must keep failing, not serve from a half-built router on the second request.
	assert.PanicsWithValue(t, "bad config", func() { do(s, "GET", "/x") })
	assert.PanicsWithValue(t, "bad config", func() { do(s, "GET", "/x") })
}

func TestPool_RequestsDoNotSeeEachOthersData(t *testing.T) {
	s := New()
	s.GET("/set/:id", func(c *Context) *Response {
		c.Set("secret", c.Param("id"))
		c.Writer.Header().Set("X-Seen", "1")
		return c.String(200, "written")
	})
	s.GET("/get", func(c *Context) *Response {
		return &Response{
			Success: true,
			Message: "clean",
			Details: map[string]any{"secret": c.Get("secret"), "param": c.Param("id"), "handled": c.Handled},
			Code:    200,
		}
	})

	do(s, "GET", "/set/1")
	resp := decode(t, do(s, "GET", "/get"))

	assert.Equal(t, map[string]any{"secret": nil, "param": "", "handled": false}, resp.Details)
}

func TestPool_PanicDoesNotPoisonLaterRequests(t *testing.T) {
	s := New()
	s.GET("/boom/:id", func(c *Context) *Response {
		c.Set("secret", c.Param("id"))
		c.Handled = true
		panic("boom")
	})
	s.GET("/next", func(c *Context) *Response {
		return &Response{Success: true, Details: c.Get("secret"), Code: 200}
	})

	assert.Panics(t, func() { do(s, "GET", "/boom/1") })

	rec := do(s, "GET", "/next")
	assert.Equal(t, 200, rec.Code)
	assert.Nil(t, decode(t, rec).Details)
}

func TestPool_CopySurvivesTheRequest(t *testing.T) {
	var copied *Context
	s := New()
	s.GET("/users/:id", func(c *Context) *Response {
		c.Set("user", "ada")
		copied = c.Copy()
		return ok(c)
	})
	s.GET("/other/:id", ok)

	do(s, "GET", "/users/1")
	do(s, "GET", "/other/2") // reuses the pooled Context

	assert.Equal(t, "1", copied.Param("id"))
	assert.Equal(t, "ada", copied.Get("user"))
}

// Run with -race: this is the test that catches a Context shared between requests.
func TestPool_ConcurrentRequestsKeepTheirOwnParams(t *testing.T) {
	s := New()
	s.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) *Response {
			c.Set("id", c.Param("id"))
			return next(c)
		}
	})
	s.GET("/users/:id/posts/:post", func(c *Context) *Response {
		return &Response{Success: true, Message: c.Get("id").(string) + "/" + c.Param("post"), Code: 200}
	})

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 200 {
				want := fmt.Sprintf("%d/%d", i, j)
				rec := do(s, "GET", "/users/"+strings.Replace(want, "/", "/posts/", 1))
				var resp server.Response
				if assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp)) {
					assert.Equal(t, want, resp.Message)
				}
			}
		}()
	}
	wg.Wait()
}

// nopWriter costs nothing, so the allocation count below is the framework's alone.
type nopWriter struct{ header http.Header }

func (w nopWriter) Header() http.Header         { return w.header }
func (w nopWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w nopWriter) WriteHeader(int)             {}

func TestServeHTTP_DoesNotAllocateOnRoutedRequests(t *testing.T) {
	s := New()
	s.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) *Response { return next(c) }
	})
	s.UseIf("/users/*", func(next HandlerFunc) HandlerFunc {
		return func(c *Context) *Response { return next(c) }
	})
	s.GET("/ping", func(c *Context) *Response { return nil })
	s.GET("/users/:id/posts/:post", func(c *Context) *Response {
		if c.Param("post") == "" {
			t.Error("param not set")
		}
		return nil
	})

	w := nopWriter{header: make(http.Header)}
	for _, target := range []string{"/ping", "/users/1/posts/2"} {
		req := httptest.NewRequest("GET", target, nil)
		do(s, "GET", target) // fill the pool

		allocs := testing.AllocsPerRun(1000, func() { s.ServeHTTP(w, req) })
		assert.Zero(t, allocs, target)
	}
}
