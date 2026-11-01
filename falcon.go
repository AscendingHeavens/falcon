package falcon

import (
	"context"
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"slices"
	"sync"

	"github.com/ascendingheavens/falcon/middleware"
	"github.com/ascendingheavens/falcon/server"
)

// New creates a new Falcon Server instance with an empty router and middleware stack.
// Example:
//
//	server := falcon.New()
//	server := falcon.New(falcon.WithDebug())
func New(options ...Option) *Server {
	s := &Server{
		router:      server.NewRouter(),
		middlewares: make([]middleware.Middleware, 0),
		routeGroups: make(map[*server.Route]*Group),
		settings:    server.DefaultSettings(),
		timeouts:    defaultTimeouts,
	}
	for _, option := range options {
		option(s)
	}
	s.pool.New = func() any {
		return server.NewContext(&s.settings)
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

// UseIf registers a middleware that only runs on routes matching one of the patterns.
// "/api/*" matches "/api" and everything below it, but not "/apiary"; a pattern with
// no wildcard matches only the route with exactly that path. Patterns are resolved
// once per route, so they cost nothing per request. They panic if malformed.
// Requests no route matches, such as 404s, don't run conditional middleware.
// Example: UseIf(AuthMiddleware(), "/api/v1/*", "/admin/*")
func (s *Server) UseIf(mw middleware.Middleware, patterns ...string) {
	s.mustNotBeBuilt("UseIf")
	s.conditionalMiddleware = append(s.conditionalMiddleware, middleware.NewConditionalMiddleware(mw, false, patterns))
}

// UseIfNot registers a middleware that runs on every route except those matching one
// of the patterns, which work as in UseIf.
// Example: UseIfNot(AuthMiddleware(), "/health", "/auth/*")
func (s *Server) UseIfNot(mw middleware.Middleware, patterns ...string) {
	s.mustNotBeBuilt("UseIfNot")
	s.conditionalMiddleware = append(s.conditionalMiddleware, middleware.NewConditionalMiddleware(mw, true, patterns))
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
		h := guardNil(rt.Handler, rt.Method+" "+rt.Path)
		if g := s.routeGroups[rt]; g != nil {
			h = wrap(h, g.Middlewares)
		}
		for i := len(s.conditionalMiddleware) - 1; i >= 0; i-- {
			cm := s.conditionalMiddleware[i]
			if cm.AppliesTo(rt.Path) {
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

// guardNil makes a handler that returns nil fail loudly, because an empty 200 would hide
// the bug. It also means middleware never gets a nil Response from a handler. A handler
// that took over the Writer has already responded, so returning nil is fine for it.
func guardNil(handler server.HandlerFunc, route string) server.HandlerFunc {
	return func(c *server.Context) *server.Response {
		resp := handler(c)
		if resp != nil {
			return resp
		}
		if c.Handled {
			return &server.Response{Success: true, Message: "Handled by the handler", Code: http.StatusOK}
		}
		return c.Error(errors.New("handler for " + route + " returned nil Response"))
	}
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

	// Execute the handler and everything wrapping it, then send what they settled on
	c.WriteResponse(handler(c))

	// A panicking handler never gets here, so its Context isn't reused.
	c.Reset(nil, nil)
	s.pool.Put(c)
}

// Start runs the HTTP server on the specified address and blocks until it stops.
// It returns nil after a Shutdown, and otherwise the error that stopped it.
//
// Example:
//
//	if err := app.Start(":8080"); err != nil {
//		log.Fatal(err)
//	}
func (s *Server) Start(addr string) error {
	log.Printf("Starting server on %s", addr)
	return ignoreClosed(s.newHTTPServer(addr).ListenAndServe())
}

// Shutdown stops the server gracefully: it stops accepting connections and waits for
// the requests in progress to finish, or for ctx to end, whichever comes first.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//	err := app.Shutdown(ctx)
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.shutdown = true
	httpServers := slices.Clone(s.httpServers)
	s.mu.Unlock()

	var errs []error
	for _, srv := range httpServers {
		errs = append(errs, srv.Shutdown(ctx))
	}
	return errors.Join(errs...)
}

// newHTTPServer creates an http.Server for this Server, with its timeouts, and
// records it so that Shutdown can stop it.
func (s *Server) newHTTPServer(addr string) *http.Server {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s,
		ReadHeaderTimeout: s.timeouts.ReadHeader,
		ReadTimeout:       s.timeouts.Read,
		WriteTimeout:      s.timeouts.Write,
		IdleTimeout:       s.timeouts.Idle,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shutdown {
		// Shutdown came first, so this server must not start listening.
		_ = srv.Close()
	}
	s.httpServers = append(s.httpServers, srv)
	return srv
}

// ignoreClosed hides the error a server always returns after a graceful Shutdown.
func ignoreClosed(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// SetRenderer sets the TemplateRenderer used by c.Render. Like middleware, it must be
// set before the first request. With WithDebug, templates reload on every render.
// Example:
//
//	renderer, err := falcon.NewTemplateRenderer("views", nil)
//	if err != nil {
//		log.Fatal(err)
//	}
//	app.SetRenderer(renderer)
func (s *Server) SetRenderer(renderer *TemplateRenderer) {
	s.mustNotBeBuilt("SetRenderer")
	s.settings.Renderer = renderer
}

// NewTemplateRenderer parses every template (.html, .tmpl, .gohtml) under dir. Templates
// are named by their path relative to dir, such as "users/show.html". Files under the
// top-level layouts/ and partials/ folders are shared with every template.
func NewTemplateRenderer(dir string, funcs template.FuncMap) (*TemplateRenderer, error) {
	return server.NewTemplateRenderer(dir, funcs)
}

// NewTemplateRendererFS is like NewTemplateRenderer but reads templates from fsys, such
// as an embed.FS, which ships them inside the binary.
func NewTemplateRendererFS(fsys fs.FS, funcs template.FuncMap) (*TemplateRenderer, error) {
	return server.NewTemplateRendererFS(fsys, funcs)
}
