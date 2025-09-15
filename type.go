package falcon

import (
	"net/http"

	"github.com/ascendingheavens/falcon/middleware"
	"github.com/ascendingheavens/falcon/server"
)

// Server is the main entry point for the Falcon framework.
// It holds the router, global middlewares, and any conditional middlewares
// that should be applied based on route patterns.
type Server struct {
	// router is the core request router responsible for mapping HTTP methods
	// and paths to handler functions.
	router *server.Router

	// middlewares is a slice of global middleware that runs on every request
	// before the matched route handler.
	middlewares []middleware.Middleware

	// conditionalMiddleware is a slice of middleware that only run when the
	// incoming request path matches the provided pattern.
	// For example, you might apply authentication middleware only for
	// `/api/*` routes.
	conditionalMiddleware []middleware.ConditionalMiddleware
}

// Group represents a collection of routes that share a common path prefix
// and middleware stack. Useful for organizing related endpoints like `/api/v1/*`.
type Group struct {
	// Prefix is the base path for this group (e.g., "/api/v1").
	Prefix string

	// Server is a reference back to the parent server, allowing
	// groups to register routes directly into the main router.
	Server *Server

	// Middlewares is a list of middleware that will be applied to
	// every route registered within this group, in addition to any
	// global or conditional middleware from the Server.
	Middlewares []middleware.Middleware
}

// Context is an alias to server.Context, which wraps the request and response
// writer and provides convenience methods (params, body parsing, etc.).
type Context = server.Context

// Response is an alias to server.Response, the unified return type
// from every handler function. Encoded as JSON and written to the client.
type Response = server.Response

// Middleware is an alias to middleware.Middleware, representing a function
// that wraps and modifies a HandlerFunc, similar to how middleware works
// in frameworks like Express or Fiber.
type Middleware = middleware.Middleware

// ConditionalMiddleware is an alias to middleware.ConditionalMiddleware,
// which pairs a pattern (e.g., "/api/*") with a Middleware function.
type ConditionalMiddleware = middleware.ConditionalMiddleware

// HandlerFunc is an alias to server.HandlerFunc, the function signature
// that route handlers must implement. It takes a *Context and returns a *Response.
type HandlerFunc = server.HandlerFunc

// TLSStarter defines an interface for starting a TLS server.
// Implementations should provide the startTLSServer method to handle
// the server startup logic for HTTPS.
type TLSStarter interface {
	startTLSServer(*http.Server)
}

// TemplateRenderer is an alias for server.TemplateRenderer.
// It is responsible for rendering HTML templates within Falcon.
type TemplateRenderer = server.TemplateRenderer

// Handler is a common interface implemented by both Server and Group.
// It provides methods for registering middleware and defining routes
// using standard HTTP methods. This allows groups and servers to be
// used interchangeably when registering routes.
type Handler interface {
	// Use registers a middleware that will be applied to all routes
	// registered through this Handler. Middleware functions are executed
	// in the order they are added, with the last registered executed first.
	//
	// Example:
	//   h.Use(LoggerMiddleware)
	//   h.Use(AuthMiddleware)
	//
	// This applies LoggerMiddleware first, then AuthMiddleware for every route.
	Use(mw middleware.Middleware)

	// Handle registers a route with the specified HTTP method and path.
	// The provided handler will be wrapped with all registered middleware
	// from both the server and group level (if applicable).
	//
	// Example:
	//   h.Handle(http.MethodGet, "/users", getUsersHandler)
	//
	// This is the lowest-level route registration function and is used
	// internally by convenience methods like GET, POST, etc.
	Handle(method, path string, handler HandlerFunc)

	// GET registers a route that matches HTTP GET requests at the given path.
	// The handler is called when an incoming request's method is GET and
	// its path matches.
	//
	// Example:
	//   h.GET("/users", getUsersHandler)
	GET(path string, handler HandlerFunc)

	// POST registers a route that matches HTTP POST requests at the given path.
	// The handler is called when an incoming request's method is POST and
	// its path matches.
	//
	// Example:
	//   h.POST("/users", createUserHandler)
	POST(path string, handler HandlerFunc)

	// PUT registers a route that matches HTTP PUT requests at the given path.
	// Typically used for replacing existing resources.
	//
	// Example:
	//   h.PUT("/users/:id", updateUserHandler)
	PUT(path string, handler HandlerFunc)

	// PATCH registers a route that matches HTTP PATCH requests at the given path.
	// Typically used for partially updating existing resources.
	//
	// Example:
	//   h.PATCH("/users/:id", partiallyUpdateUserHandler)
	PATCH(path string, handler HandlerFunc)

	// DELETE registers a route that matches HTTP DELETE requests at the given path.
	// Typically used for deleting resources.
	//
	// Example:
	//   h.DELETE("/users/:id", deleteUserHandler)
	DELETE(path string, handler HandlerFunc)
}
