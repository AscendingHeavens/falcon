package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// namedRouter registers each path under GET with a handler reporting that path,
// so tests can tell which route actually matched.
func namedRouter(paths ...string) *Router {
	r := NewRouter()
	for _, p := range paths {
		r.Handle("GET", p, func(c *Context) *Response {
			return &Response{Message: p}
		})
	}
	return r
}

func TestRouter_Matching(t *testing.T) {
	r := namedRouter(
		"/",
		"/users",
		"/users/me",
		"/users/me/settings",
		"/users/:id",
		"/users/:id/profile",
		"/uploads",
		"/static/*filepath",
		"/files/:name/*rest",
		"/a:b",
		"/x*y",
		"/any/*all",
		"/any/known",
		"/any/:one/known",
	)

	tests := []struct {
		name       string
		path       string
		wantRoute  string
		wantParams Params
	}{
		{"root", "/", "/", nil},
		{"static", "/users", "/users", nil},
		{"shared prefix is split correctly", "/uploads", "/uploads", nil},
		{"param", "/users/42", "/users/:id", Params{{"id", "42"}}},
		{"static beats param", "/users/me", "/users/me", nil},
		{"param value sharing a static prefix", "/users/mel", "/users/:id", Params{{"id", "mel"}}},
		{"backtracks from static to param", "/users/me/profile", "/users/:id/profile", Params{{"id", "me"}}},
		{"static nested under static", "/users/me/settings", "/users/me/settings", nil},
		{"catch-all", "/static/css/a.css", "/static/*filepath", Params{{"filepath", "css/a.css"}}},
		{"catch-all matches nothing", "/static/", "/static/*filepath", Params{{"filepath", ""}}},
		{"catch-all after param", "/files/a/b/c", "/files/:name/*rest", Params{{"name", "a"}, {"rest", "b/c"}}},
		{"colon inside a segment is literal", "/a:b", "/a:b", nil},
		{"star inside a segment is literal", "/x*y", "/x*y", nil},
		{"static beats catch-all", "/any/known", "/any/known", nil},
		{"param beats catch-all", "/any/z/known", "/any/:one/known", Params{{"one", "z"}}},
		{"backtracks to catch-all and drops the param", "/any/z/other", "/any/*all", Params{{"all", "z/other"}}},
		{"unicode param", "/users/दिल्ली", "/users/:id", Params{{"id", "दिल्ली"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var params Params
			h := r.FindHandler("GET", tt.path, &params)
			if assert.NotNil(t, h) {
				assert.Equal(t, tt.wantRoute, h(nil).Message)
			}
			assert.Equal(t, tt.wantParams, params)
		})
	}
}

func TestRouter_NoMatch(t *testing.T) {
	r := namedRouter("/users", "/users/:id", "/users/:id/profile", "/static/*filepath")

	for _, path := range []string{
		"",
		"/use",
		"/usersx",
		"/users/",           // a param never matches an empty segment
		"/users//profile",   // same, in the middle of a path
		"/users/1/profile/", // trailing slash is a different path
		"/users/1/other",
		"/static", // the catch-all needs its slash
		"/nope",
	} {
		t.Run(path, func(t *testing.T) {
			var params Params
			assert.Nil(t, r.FindHandler("GET", path, &params))
			assert.Empty(t, params)
		})
	}

	var params Params
	assert.Nil(t, r.FindHandler("POST", "/users", &params), "other methods have their own tree")
}

func TestRouter_RegistrationOrderDoesNotMatter(t *testing.T) {
	paths := []string{"/users/:id", "/users/me", "/users", "/uploads", "/u"}
	reversed := []string{"/u", "/uploads", "/users", "/users/me", "/users/:id"}

	for _, r := range []*Router{namedRouter(paths...), namedRouter(reversed...)} {
		for path, want := range map[string]string{
			"/users/me": "/users/me",
			"/users/1":  "/users/:id",
			"/users":    "/users",
			"/uploads":  "/uploads",
			"/u":        "/u",
		} {
			var params Params
			h := r.FindHandler("GET", path, &params)
			if assert.NotNil(t, h, path) {
				assert.Equal(t, want, h(nil).Message)
			}
		}
	}
}

func TestRouter_ConflictsPanicAndNameBothRoutes(t *testing.T) {
	noop := func(c *Context) *Response { return nil }

	tests := []struct {
		name  string
		first string
		then  string
		want  []string
	}{
		{"duplicate static", "/users", "/users", []string{"GET /users", "twice"}},
		{"duplicate param", "/users/:id", "/users/:id", []string{"GET /users/:id", "twice"}},
		{"different param names", "/users/:id", "/users/:name", []string{"GET /users/:name", "GET /users/:id", ":name", ":id"}},
		{"different names under a shared param", "/users/:id/a", "/users/:name/b", []string{"GET /users/:name/b", "GET /users/:id/a"}},
		{"two catch-alls", "/static/*a", "/static/*b", []string{"GET /static/*b", "GET /static/*a"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRouter()
			r.Handle("GET", tt.first, noop)

			defer func() {
				msg, ok := recover().(string)
				assert.True(t, ok, "should panic with a message")
				for _, want := range tt.want {
					assert.Contains(t, msg, want)
				}
			}()
			r.Handle("GET", tt.then, noop)
		})
	}
}

func TestRouter_SameRouteOnAnotherMethodIsNotAConflict(t *testing.T) {
	noop := func(c *Context) *Response { return nil }
	r := NewRouter()
	r.Handle("GET", "/users/:id", noop)
	assert.NotPanics(t, func() { r.Handle("POST", "/users/:name", noop) })
}

func TestRouter_InvalidRoutesPanic(t *testing.T) {
	noop := func(c *Context) *Response { return nil }

	for _, path := range []string{"", "users", "/users/:", "/static/*", "/static/*file/more"} {
		t.Run(path, func(t *testing.T) {
			assert.Panics(t, func() { NewRouter().Handle("GET", path, noop) })
		})
	}
}

func TestRouter_AllowedMethods(t *testing.T) {
	noop := func(c *Context) *Response { return nil }
	r := NewRouter()
	r.Handle("GET", "/users/:id", noop)
	r.Handle("DELETE", "/users/:id", noop)
	r.Handle("POST", "/users", noop)
	r.Handle("OPTIONS", "/custom", noop)
	r.Handle("HEAD", "/head", noop)
	r.Handle("GET", "/head", noop)

	assert.Equal(t, []string{"DELETE", "GET", "HEAD", "OPTIONS"}, r.AllowedMethods("/users/1"))
	assert.Equal(t, []string{"OPTIONS", "POST"}, r.AllowedMethods("/users"))
	assert.Equal(t, []string{"OPTIONS"}, r.AllowedMethods("/custom"))
	assert.Equal(t, []string{"GET", "HEAD", "OPTIONS"}, r.AllowedMethods("/head"))
	assert.Nil(t, r.AllowedMethods("/nope"))
}

func TestRouter_FindHandlerDoesNotAllocate(t *testing.T) {
	r := namedRouter("/users", "/users/me", "/users/:id", "/users/:id/posts/:post", "/static/*filepath", "/uploads")
	params := make(Params, 0, 8)

	for _, path := range []string{"/users", "/users/42", "/users/42/posts/7", "/static/a/b.css", "/users/42/nope", "/nope"} {
		t.Run(path, func(t *testing.T) {
			allocs := testing.AllocsPerRun(100, func() {
				params = params[:0]
				r.FindHandler("GET", path, &params)
			})
			assert.Zero(t, allocs)
		})
	}
}

func TestContext_Reset(t *testing.T) {
	c := &Context{Params: make(Params, 0, 8)}
	c.Reset(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	c.Params = append(c.Params, Param{"id", "1"})
	c.Handled = true
	c.Set("user", "ada")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/other", nil)
	c.Reset(w, r)

	assert.Equal(t, http.ResponseWriter(w), c.Writer)
	assert.Equal(t, r, c.Request)
	assert.Empty(t, c.Params)
	assert.Equal(t, 8, cap(c.Params), "capacity is kept so pooled Contexts don't reallocate")
	assert.False(t, c.Handled)
	assert.Nil(t, c.Get("user"), "values must not leak into the next request")
	assert.Equal(t, "", c.Param("id"), "params must not leak into the next request")
}

func TestContext_Copy(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &Context{}
	c.Reset(rec, httptest.NewRequest("GET", "/", nil))
	c.Params = append(c.Params, Param{"id", "1"})
	c.Set("user", "ada")

	cp := c.Copy()
	c.Reset(nil, nil) // what the server does once the handler returns
	c.Params = append(c.Params, Param{"id", "someone else's"})

	assert.Equal(t, "1", cp.Param("id"), "params must survive the original being reused")
	assert.Equal(t, "ada", cp.Get("user"), "values must survive the original being reused")

	_, err := cp.Writer.Write([]byte("late"))
	assert.NoError(t, err)
	cp.Writer.Header().Set("X-Late", "1")
	cp.Writer.WriteHeader(500)
	assert.Empty(t, rec.Body.String(), "a copy must not write to the client")
	assert.Equal(t, 200, rec.Code)
	assert.Empty(t, rec.Header().Get("X-Late"))
}
