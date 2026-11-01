// Package bench compares Falcon against Gin and Echo.
//
// Run with: go test -bench . -benchmem
// It lives in its own module so Gin and Echo never become dependencies of Falcon.
package bench

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ascendingheavens/falcon"
	"github.com/gin-gonic/gin"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

// envelope mirrors falcon.Response, so all three frameworks encode the same JSON.
type envelope struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
	Code    int    `json:"code"`
}

type user struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

var (
	benchUser = user{ID: 42, Name: "Ada Lovelace", Email: "ada@example.com"}

	// paramSink keeps param reads from being optimized away, and lets the tests
	// check that the param really was extracted.
	paramSink string

	// handledResponse lets Falcon's empty handlers write nothing and allocate nothing,
	// like empty Gin and Echo handlers, so those benchmarks measure routing alone.
	handledResponse = &falcon.Response{Success: true, Code: 200}
)

// framework builds an http.Handler for a route set, one way per kind of handler.
type framework struct {
	name string
	// noop registers handlers that do nothing, to measure routing alone.
	noop func(routes []route) http.Handler
	// param registers handlers that read the "id" param.
	param func(routes []route) http.Handler
	// json registers handlers that respond with the same JSON envelope.
	json func(routes []route) http.Handler
}

func newFalcon(routes []route, h falcon.HandlerFunc) http.Handler {
	app := falcon.New()
	for _, r := range routes {
		app.Handle(r.method, r.path, h)
	}
	return app
}

func newGin(routes []route, h gin.HandlerFunc) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	app := gin.New()
	for _, r := range routes {
		app.Handle(r.method, r.path, h)
	}
	return app
}

func newEcho(routes []route, h echo.HandlerFunc) http.Handler {
	app := echo.New()
	for _, r := range routes {
		app.Add(r.method, r.path, h)
	}
	return app
}

var frameworks = []framework{
	{
		name: "falcon",
		noop: func(routes []route) http.Handler {
			return newFalcon(routes, func(c *falcon.Context) *falcon.Response {
				c.Handled = true
				return handledResponse
			})
		},
		param: func(routes []route) http.Handler {
			return newFalcon(routes, func(c *falcon.Context) *falcon.Response {
				paramSink = c.Param("id")
				c.Handled = true
				return handledResponse
			})
		},
		json: func(routes []route) http.Handler {
			return newFalcon(routes, func(c *falcon.Context) *falcon.Response {
				return &falcon.Response{Success: true, Message: "User found", Details: benchUser, Code: 200}
			})
		},
	},
	{
		name: "gin",
		noop: func(routes []route) http.Handler {
			return newGin(routes, func(c *gin.Context) {})
		},
		param: func(routes []route) http.Handler {
			return newGin(routes, func(c *gin.Context) { paramSink = c.Param("id") })
		},
		json: func(routes []route) http.Handler {
			return newGin(routes, func(c *gin.Context) {
				c.JSON(200, envelope{Success: true, Message: "User found", Details: benchUser, Code: 200})
			})
		},
	},
	{
		name: "echo",
		noop: func(routes []route) http.Handler {
			return newEcho(routes, func(c echo.Context) error { return nil })
		},
		param: func(routes []route) http.Handler {
			return newEcho(routes, func(c echo.Context) error {
				paramSink = c.Param("id")
				return nil
			})
		},
		json: func(routes []route) http.Handler {
			return newEcho(routes, func(c echo.Context) error {
				return c.JSON(200, envelope{Success: true, Message: "User found", Details: benchUser, Code: 200})
			})
		},
	},
}

// discardWriter is a ResponseWriter with no cost of its own, so results measure
// the framework and not the recorder. It keeps the status and body size for the tests.
type discardWriter struct {
	header http.Header
	status int
	size   int
	body   []byte
	keep   bool
}

func newDiscardWriter() *discardWriter {
	return &discardWriter{header: make(http.Header)}
}

func (w *discardWriter) Header() http.Header { return w.header }

func (w *discardWriter) WriteHeader(code int) { w.status = code }

func (w *discardWriter) Write(p []byte) (int, error) {
	w.size += len(p)
	if w.keep {
		w.body = append(w.body, p...)
	}
	return len(p), nil
}

func requests(routes []route) []*http.Request {
	reqs := make([]*http.Request, len(routes))
	for i, r := range routes {
		reqs[i] = httptest.NewRequest(r.method, r.url(), nil)
	}
	return reqs
}

// run reports the cost of one pass over reqs as a single operation.
func run(b *testing.B, h http.Handler, reqs []*http.Request) {
	b.Helper()
	w := newDiscardWriter()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, r := range reqs {
			h.ServeHTTP(w, r)
		}
	}
}

var (
	staticRoute = []route{{"GET", "/ping"}}
	paramRoute  = []route{{"GET", "/users/:id"}}
)

// A single static route: the cost of the framework with nothing to search.
func BenchmarkStatic(b *testing.B) {
	for _, f := range frameworks {
		b.Run(f.name, func(b *testing.B) {
			run(b, f.noop(staticRoute), requests(staticRoute))
		})
	}
}

// A single param route, reading the param in the handler.
func BenchmarkParam(b *testing.B) {
	for _, f := range frameworks {
		b.Run(f.name, func(b *testing.B) {
			run(b, f.param(paramRoute), requests(paramRoute))
		})
	}
}

// A static route looked up in the full GitHub route set.
func BenchmarkGitHubStatic(b *testing.B) {
	target := []route{{"GET", "/user/repos"}}
	for _, f := range frameworks {
		b.Run(f.name, func(b *testing.B) {
			run(b, f.noop(githubRoutes), requests(target))
		})
	}
}

// A route with three params looked up in the full GitHub route set.
func BenchmarkGitHubParam(b *testing.B) {
	target := []route{{"GET", "/repos/:owner/:repo/issues/:number"}}
	for _, f := range frameworks {
		b.Run(f.name, func(b *testing.B) {
			run(b, f.noop(githubRoutes), requests(target))
		})
	}
}

// One request to every route in the GitHub route set, counted as one operation.
func BenchmarkGitHubAll(b *testing.B) {
	for _, f := range frameworks {
		b.Run(f.name, func(b *testing.B) {
			run(b, f.noop(githubRoutes), requests(githubRoutes))
		})
	}
}

// A full request: route lookup, param route, and a JSON envelope response.
func BenchmarkJSON(b *testing.B) {
	for _, f := range frameworks {
		b.Run(f.name, func(b *testing.B) {
			run(b, f.json(paramRoute), requests(paramRoute))
		})
	}
}

// The tests below check the benchmarks measure what they claim to. A benchmark
// that silently hits a 404 or encodes a different body would make the numbers meaningless.

func TestEveryGitHubRouteMatches(t *testing.T) {
	t.Logf("%d routes", len(githubRoutes))
	reqs := requests(githubRoutes)
	for _, f := range frameworks {
		t.Run(f.name, func(t *testing.T) {
			h := f.noop(githubRoutes)
			for i, r := range reqs {
				w := newDiscardWriter()
				h.ServeHTTP(w, r)
				assert.NotContains(t, []int{404, 405}, w.status, "%s %s", githubRoutes[i].method, githubRoutes[i].path)
			}
		})
	}
}

func TestParamIsRead(t *testing.T) {
	reqs := []*http.Request{httptest.NewRequest("GET", "/users/42", nil)}
	for _, f := range frameworks {
		t.Run(f.name, func(t *testing.T) {
			paramSink = ""
			f.param(paramRoute).ServeHTTP(newDiscardWriter(), reqs[0])
			assert.Equal(t, "42", paramSink)
		})
	}
}

func TestJSONBodiesMatch(t *testing.T) {
	want := `{"success":true,"message":"User found","details":{"id":42,"name":"Ada Lovelace","email":"ada@example.com"},"code":200}`
	for _, f := range frameworks {
		t.Run(f.name, func(t *testing.T) {
			w := newDiscardWriter()
			w.keep = true
			f.json(paramRoute).ServeHTTP(w, httptest.NewRequest("GET", "/users/42", nil))
			assert.Equal(t, 200, w.status)
			assert.JSONEq(t, want, string(w.body))
		})
	}
}
