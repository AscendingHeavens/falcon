package falcon

import (
	"net/http"

	"github.com/ascendingheavens/falcon/middleware"
)

// Group represents a collection of routes sharing a common prefix
// and middleware stack. Useful for organizing related endpoints.
// Example: v1 := app.Group("/api/v1")
func (s *Server) Group(prefix string) *Group {
	return &Group{
		Prefix:      prefix,
		Server:      s,
		Middlewares: make([]middleware.Middleware, 0),
	}
}

// Use registers a middleware for this specific group.
// These middlewares are applied only to routes within the group,
// in addition to any global middleware from the parent server.
func (g *Group) Use(mw middleware.Middleware) {
	g.Server.mustNotBeBuilt("Group.Use")
	g.Middlewares = append(g.Middlewares, mw)
}

// Handle registers a route for the group with a specific HTTP method and path.
// It automatically prepends the group's prefix to the path. The group's middleware
// applies to the route no matter whether it was added before or after.
func (g *Group) Handle(method, path string, handler HandlerFunc) {
	g.Server.mustNotBeBuilt("Group.Handle")
	rt := g.Server.router.Handle(method, g.Prefix+path, handler)
	g.Server.routeGroups[rt] = g
}

// GET registers a route with the HTTP GET method for this group.
// The handler is invoked when a request matches the given path.
func (g *Group) GET(path string, handler HandlerFunc) {
	g.Handle(http.MethodGet, path, handler)
}

// POST registers a route with the HTTP POST method for this group.
// The handler is invoked when a request matches the given path.
func (g *Group) POST(path string, handler HandlerFunc) {
	g.Handle(http.MethodPost, path, handler)
}

// PUT registers a route with the HTTP PUT method for this group.
// The handler is invoked when a request matches the given path.
func (g *Group) PUT(path string, handler HandlerFunc) {
	g.Handle(http.MethodPut, path, handler)
}

// PATCH registers a route with the HTTP PATCH method for this group.
// The handler is invoked when a request matches the given path.
func (g *Group) PATCH(path string, handler HandlerFunc) {
	g.Handle(http.MethodPatch, path, handler)
}

// DELETE registers a route with the HTTP DELETE method for this group.
// The handler is invoked when a request matches the given path.
func (g *Group) DELETE(path string, handler HandlerFunc) {
	g.Handle(http.MethodDelete, path, handler)
}
