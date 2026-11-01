package server

import (
	"net/http"

	"github.com/go-playground/validator/v10"
)

// Route represents a single registered route in the router.
// It contains the HTTP method, the route path pattern, and the handler function.
type Route struct {
	Method  string      // HTTP method (GET, POST, PUT, etc.)
	Path    string      // Route pattern, e.g. "/users/:id"
	Handler HandlerFunc // Function to handle requests matching this route
}

// Router is an HTTP router backed by one radix tree per HTTP method. It supports
// static segments, path parameters (/users/:id) and catch-alls (/static/*filepath).
// Lookups don't allocate and their cost depends on the path, not the number of routes.
type Router struct {
	trees  []methodTree // One tree per method; a short slice is faster than a map here
	routes []*Route     // List of all registered routes
}

// methodTree is the radix tree holding every route of one HTTP method.
type methodTree struct {
	method string
	root   *node
}

// Param is a single path parameter extracted from a request path.
type Param struct {
	Key   string
	Value string
}

// Params holds the path parameters of a request, in the order they appear in the route.
// A slice is used instead of a map because routes have few params: a short scan is
// faster than a map lookup and a pooled slice doesn't allocate.
type Params []Param

// Get returns the value of the named parameter, or "" if it isn't present.
func (p Params) Get(name string) string {
	for i := range p {
		if p[i].Key == name {
			return p[i].Value
		}
	}
	return ""
}

// Response is the unified return type for all handlers in Falcon.
// It is automatically serialized to JSON and written to the client.
// Fields:
//   - Success: indicates whether the request was successful.
//   - Message: human-readable message describing the result.
//   - Details: optional field containing extra data (any type).
//   - Code: HTTP status code to be sent to the client. It's Required to have at least one Status code
type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
	Code    int    `json:"code"` // required
}

// Context wraps http.ResponseWriter and *http.Request, providing
// convenience access to route parameters and helper methods in the future.
// Fields:
//   - Writer: the http.ResponseWriter to write responses.
//   - Request: the incoming HTTP request.
//   - Params: the path parameters extracted from the route (e.g., ":id").
//
// Contexts are pooled and reused once the handler returns. A goroutine that outlives
// the handler must use Copy, not the Context it was given.
type Context struct {
	Writer  http.ResponseWriter
	Request *http.Request
	Params  Params
	Handled bool
	// Templates *TemplateRenderer To be added
	Validator *validator.Validate
	Values    map[string]any
}

// HandlerFunc defines the signature for all route handlers in Falcon.
// Every handler receives a pointer to a Context and returns a pointer to a Response.
// The framework automatically writes the Response as JSON to the client.
type HandlerFunc func(c *Context) *Response
