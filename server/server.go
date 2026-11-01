package server

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
)

// NewRouter creates and returns a new Router instance.
func NewRouter() *Router {
	return &Router{}
}

// Handle registers a new route with a specific HTTP method, path, and handler.
// It panics if the path doesn't start with "/" or conflicts with an existing route,
// so mistakes surface at startup instead of on some later request.
// The returned Route's Handler can be replaced until the router starts serving.
func (r *Router) Handle(method, path string, handler HandlerFunc) *Route {
	rt := &Route{Method: method, Path: path, Handler: handler}
	if len(path) == 0 || path[0] != '/' {
		panic(fmt.Sprintf("falcon: route %q must start with \"/\"", routeName(rt)))
	}

	var root *node
	for i := range r.trees {
		if r.trees[i].method == method {
			root = r.trees[i].root
			break
		}
	}
	if root == nil {
		root = &node{}
		r.trees = append(r.trees, methodTree{method: method, root: root})
	}

	root.insert(rt)
	r.routes = append(r.routes, rt)
	return rt
}

// Routes returns every registered route.
func (r *Router) Routes() []*Route {
	return r.routes
}

// FindHandler attempts to match an incoming request (method + path)
// against the registered routes and appends any path parameters to params.
// It returns nil if no route matches, leaving params unchanged.
func (r *Router) FindHandler(method, path string, params *Params) HandlerFunc {
	for i := range r.trees {
		if r.trees[i].method == method {
			if rt := r.trees[i].root.find(path, params); rt != nil {
				return rt.Handler
			}
			return nil
		}
	}
	return nil
}

// AllowedMethods returns the sorted methods a path can be requested with, or nil if
// no route matches it. HEAD is included when GET is, and OPTIONS is always included,
// because the server answers both automatically.
func (r *Router) AllowedMethods(path string) []string {
	var allowed []string
	var params Params
	for i := range r.trees {
		params = params[:0]
		if r.trees[i].root.find(path, &params) != nil {
			allowed = append(allowed, r.trees[i].method)
		}
	}
	if allowed == nil {
		return nil
	}

	if slices.Contains(allowed, http.MethodGet) && !slices.Contains(allowed, http.MethodHead) {
		allowed = append(allowed, http.MethodHead)
	}
	if !slices.Contains(allowed, http.MethodOptions) {
		allowed = append(allowed, http.MethodOptions)
	}
	slices.Sort(allowed)
	return allowed
}

// Set stores a value in the Context under the specified key.
// Example: c.Set("user", claims)
func (c *Context) Set(key string, value any) {
	if c.Values == nil {
		c.Values = make(map[string]any)
	}
	c.Values[key] = value
}

// Get retrieves a value from the Context by key.
// Returns nil if the key does not exist.
// Example: user := c.Get("user")
func (c *Context) Get(key string) any {
	if c.Values == nil {
		return nil
	}
	return c.Values[key]
}

// Reset prepares a pooled Context for a new request, keeping the Params capacity
// so that later requests don't allocate. Passing nil clears it for return to the pool.
func (c *Context) Reset(w http.ResponseWriter, r *http.Request) {
	c.Writer = w
	c.Request = r
	c.Params = c.Params[:0]
	c.Handled = false
	c.Validator = nil
	c.Values = nil
}

// Copy returns a Context that stays valid after the handler returns, for use in goroutines:
//
//	cp := c.Copy()
//	go audit(cp.Param("id"), cp.Get("user"))
//
// The copy can't respond to the client: anything written to it is discarded.
// Its Request is shared, and net/http closes the request's Body and cancels its
// context once the handler returns, so read what you need from those beforehand.
func (c *Context) Copy() *Context {
	return &Context{
		Writer:    discardWriter{header: make(http.Header)},
		Request:   c.Request,
		Params:    slices.Clone(c.Params),
		Handled:   true,
		Validator: c.Validator,
		Values:    maps.Clone(c.Values),
	}
}

// discardWriter is the http.ResponseWriter of a copied Context.
type discardWriter struct {
	header http.Header
}

func (w discardWriter) Header() http.Header         { return w.header }
func (w discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w discardWriter) WriteHeader(int)             {}
