package falcon

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/ascendingheavens/falcon/middleware"
	"github.com/ascendingheavens/falcon/server"
)

// New creates a new Falcon Server instance with an empty router and middleware stack.
// Example:
//
//	server := falcon.New()
func New() *Server {
	return &Server{
		router:      server.NewRouter(),
		middlewares: make([]middleware.Middleware, 0),
	}
}

// Use registers a global middleware that will run on every request.
// Middleware is executed in the order it is added.
// Example:
//
//	server.Use(middleware.CORS())
func (s *Server) Use(mw middleware.Middleware) {
	s.middlewares = append(s.middlewares, mw)
}

// UseIf registers a conditional middleware that only runs if the request path
// matches the given pattern. Patterns can include a wildcard '*' at the end.
// Example: UseIf("/api/v1/*", AuthMiddleware())
func (s *Server) UseIf(pattern string, mw middleware.Middleware) {
	s.conditionalMiddleware = append(s.conditionalMiddleware, middleware.ConditionalMiddleware{
		Pattern:    pattern,
		Middleware: mw,
	})
}

// Handle registers a route with a specific HTTP method and path.
// Global middleware is automatically applied in reverse order (so execution order is correct).
func (s *Server) Handle(method, path string, handler server.HandlerFunc) {
	combined := handler
	for i := len(s.middlewares) - 1; i >= 0; i-- {
		combined = s.middlewares[i](combined)
	}
	s.router.Handle(method, path, combined)
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
	c := &server.Context{Writer: w, Request: r}

	// Handle OPTIONS preflight before route matching
	if r.Method == http.MethodOptions {
		// Create a dummy handler for OPTIONS
		handler := func(c *server.Context) *server.Response {
			return &server.Response{Success: true, Message: "OK", Code: http.StatusNoContent}
		}

		// Apply global middleware (in reverse order, like in Handle())
		combined := handler
		for i := len(s.middlewares) - 1; i >= 0; i-- {
			combined = s.middlewares[i](combined)
		}

		// Apply conditional middleware
		final := combined
		for _, cm := range s.conditionalMiddleware {
			if strings.HasPrefix(r.URL.Path, strings.TrimSuffix(cm.Pattern, "*")) {
				final = cm.Middleware(final)
			}
		}

		// Execute the handler chain
		resp := final(c)
		if !c.Handled && resp != nil {
			w.WriteHeader(resp.Code)
		}
		return
	}

	// Find the matching handler and path parameters
	handler, params := s.router.FindHandler(r.Method, r.URL.Path)
	if handler == nil {
		http.NotFound(w, r)
		return
	}
	c.Params = params

	// Apply conditional middleware if the request path matches any pattern
	final := handler
	for _, cm := range s.conditionalMiddleware {
		if strings.HasPrefix(r.URL.Path, strings.TrimSuffix(cm.Pattern, "*")) {
			final = cm.Middleware(final)
		}
	}

	// Execute the handler
	resp := final(c)

	// Write JSON response
	if !c.Handled && resp != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.Code)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("failed to encode JSON response: %v", err)
		}
	}
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
