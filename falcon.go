package falcon

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/ascendingheavens/falcon/middleware"
	"github.com/ascendingheavens/falcon/server"
)

// New creates a new Falcon Server instance with an empty router and middleware stack.
// Example:
//
//	server := falcon.New()
func New() *Server {
	s := &Server{
		router:      server.NewRouter(),
		middlewares: make([]middleware.Middleware, 0),
		routeGroups: make(map[*server.Route]*Group),
	}
	s.pool.New = func() any {
		return &server.Context{Params: make(server.Params, 0, 8)}
	}
	// OnceFunc repeats a panic on every call, so a middleware that panics while
	// being built fails every request instead of leaving a half-built server.
	s.ensureBuilt = sync.OnceFunc(s.buildChains)
	return s
}

// Use registers a global middleware that will run on every request, including
// the ones no route matches. Middleware is executed in the order it is added,
// and applies to every route no matter whether it was registered before or after.
// Example:
//
//	server.Use(middleware.CORS())
func (s *Server) Use(mw middleware.Middleware) {
	s.mustNotBeBuilt("Use")
	s.middlewares = append(s.middlewares, mw)
}

// UseIf registers a conditional middleware that only runs on routes whose path
// matches the given pattern. Patterns can include a wildcard '*' at the end.
// Example: UseIf("/api/v1/*", AuthMiddleware())
func (s *Server) UseIf(pattern string, mw middleware.Middleware) {
	s.mustNotBeBuilt("UseIf")
	s.conditionalMiddleware = append(s.conditionalMiddleware, middleware.ConditionalMiddleware{
		Pattern:    pattern,
		Middleware: mw,
	})
}

// Handle registers a route with a specific HTTP method and path.
// It panics if the route conflicts with one that is already registered.
func (s *Server) Handle(method, path string, handler server.HandlerFunc) {
	s.mustNotBeBuilt("Handle")
	s.router.Handle(method, path, handler)
}

// mustNotBeBuilt panics if the server has started serving: the chains are already
// built, and changing them would race with the requests in flight.
func (s *Server) mustNotBeBuilt(call string) {
	if s.built.Load() {
		panic("falcon: " + call + " called after the server started serving; register everything before the first request")
	}
}

// buildChains wraps every route in its middleware. It runs once, on the first request.
// The order, outermost first, is: global, conditional, group, handler.
func (s *Server) buildChains() {
	s.built.Store(true)

	for _, rt := range s.router.Routes() {
		h := rt.Handler
		if g := s.routeGroups[rt]; g != nil {
			h = wrap(h, g.Middlewares)
		}
		for i := len(s.conditionalMiddleware) - 1; i >= 0; i-- {
			cm := s.conditionalMiddleware[i]
			if strings.HasPrefix(rt.Path, strings.TrimSuffix(cm.Pattern, "*")) {
				h = cm.Middleware(h)
			}
		}
		rt.Handler = wrap(h, s.middlewares)
	}

	s.notFound = wrap(notFoundHandler, s.middlewares)
	s.methodNotAllowed = wrap(s.methodNotAllowedHandler, s.middlewares)
	s.autoOptions = wrap(s.autoOptionsHandler, s.middlewares)
	s.slashRedirect = wrap(slashRedirectHandler, s.middlewares)
}

// wrap applies middlewares to h in reverse, so that the first one added runs first.
func wrap(h server.HandlerFunc, middlewares []middleware.Middleware) server.HandlerFunc {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// GET registers a route with the HTTP GET method on the server.
// The handler is invoked when a request matches the given path.
func (s *Server) GET(path string, handler server.HandlerFunc) {
	s.Handle(http.MethodGet, path, handler)
}

// POST registers a route with the HTTP POST method on the server.
// The handler is invoked when a request matches the given path.
func (s *Server) POST(path string, handler server.HandlerFunc) {
	s.Handle(http.MethodPost, path, handler)
}

// PUT registers a route with the HTTP PUT method on the server.
// The handler is invoked when a request matches the given path.
func (s *Server) PUT(path string, handler server.HandlerFunc) {
	s.Handle(http.MethodPut, path, handler)
}

// PATCH registers a route with the HTTP PATCH method on the server.
// The handler is invoked when a request matches the given path.
func (s *Server) PATCH(path string, handler server.HandlerFunc) {
	s.Handle(http.MethodPatch, path, handler)
}

// DELETE registers a route with the HTTP DELETE method on the server.
// The handler is invoked when a request matches the given path.
func (s *Server) DELETE(path string, handler server.HandlerFunc) {
	s.Handle(http.MethodDelete, path, handler)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.ensureBuilt()

	c := s.pool.Get().(*server.Context)
	c.Reset(w, r)

	// Find the matching handler and path parameters
	handler := s.router.FindHandler(r.Method, r.URL.Path, &c.Params)
	if handler == nil {
		handler = s.fallback(c)
	}

	// Execute the handler
	resp := handler(c)

	// Write JSON response
	if !c.Handled && resp != nil {
		c.Writer.Header().Set("Content-Type", "application/json")
		c.Writer.WriteHeader(resp.Code)
		if err := json.NewEncoder(c.Writer).Encode(resp); err != nil {
			log.Printf("failed to encode JSON response: %v", err)
		}
	}

	// A panicking handler never gets here, so its Context isn't reused.
	c.Reset(nil, nil)
	s.pool.Put(c)
}

// Start runs the HTTP server on the specified address. It logs the startup
// and will terminate the program if ListenAndServe returns an error.
func (s *Server) Start(addr string) {
	log.Printf("Starting server on %s", addr)
	if err := http.ListenAndServe(addr, s); err != nil {
		log.Fatal(err)
	}
}

// NewTemplateRenderer Encapsulates server.NewTemplateRenderer
// func NewTemplateRenderer(pattern string, devMode bool, funcs template.FuncMap) *server.TemplateRenderer {
// 	return server.NewTemplateRenderer(pattern, devMode, funcs)
// }
// To be added
