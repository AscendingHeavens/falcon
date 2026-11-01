package falcon

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/ascendingheavens/falcon/server"
)

// fallback picks the handler for a request no route matched exactly. It's only
// reached on the slow path, so unlike route lookup it's allowed to allocate.
func (s *Server) fallback(c *server.Context) server.HandlerFunc {
	method := c.Request.Method
	path := c.Request.URL.Path

	if method == http.MethodOptions {
		if s.router.AllowedMethods(path) != nil {
			return s.autoOptions
		}
		return s.notFound
	}

	if method == http.MethodHead {
		// HEAD is a GET without a body, on every response this function can pick.
		c.Writer = headWriter{c.Writer}
		method = http.MethodGet
		if h := s.router.FindHandler(method, path, &c.Params); h != nil {
			return h
		}
	}

	if alt, ok := toggleSlash(path); ok && s.router.FindHandler(method, alt, &c.Params) != nil {
		c.Params = c.Params[:0]
		return s.slashRedirect
	}

	if s.router.AllowedMethods(path) != nil {
		return s.methodNotAllowed
	}
	return s.notFound
}

func notFoundHandler(c *server.Context) *server.Response {
	return &server.Response{Success: false, Message: "Not Found", Code: http.StatusNotFound}
}

func (s *Server) methodNotAllowedHandler(c *server.Context) *server.Response {
	c.Writer.Header().Set("Allow", strings.Join(s.router.AllowedMethods(c.Request.URL.Path), ", "))
	return &server.Response{Success: false, Message: "Method Not Allowed", Code: http.StatusMethodNotAllowed}
}

// autoOptionsHandler answers OPTIONS on a path that has routes but no OPTIONS route of its own.
func (s *Server) autoOptionsHandler(c *server.Context) *server.Response {
	c.Writer.Header().Set("Allow", strings.Join(s.router.AllowedMethods(c.Request.URL.Path), ", "))
	c.Writer.WriteHeader(http.StatusNoContent)
	c.Handled = true
	return &server.Response{Success: true, Message: "OK", Code: http.StatusNoContent}
}

// slashRedirectHandler redirects to the same path with its trailing slash added or removed.
func slashRedirectHandler(c *server.Context) *server.Response {
	code := http.StatusPermanentRedirect // Keeps the method and body, unlike a 301 in browsers
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
		code = http.StatusMovedPermanently
	}

	// Going through url.URL escapes the path. Without it a request for "/\evil.com/"
	// would redirect to "/\evil.com", which browsers read as another site.
	alt, _ := toggleSlash(c.Request.URL.Path)
	target := url.URL{Path: alt, RawQuery: c.Request.URL.RawQuery}
	return c.Redirect(code, target.String())
}

// toggleSlash returns path with its trailing slash added or removed.
// It reports false for paths where that makes no sense.
func toggleSlash(path string) (string, bool) {
	if path == "" || path == "/" {
		return "", false
	}
	if path[len(path)-1] == '/' {
		return path[:len(path)-1], true
	}
	return path + "/", true
}

// headWriter drops the body of a response to a HEAD request.
type headWriter struct {
	http.ResponseWriter
}

func (w headWriter) Write(p []byte) (int, error) { return len(p), nil }
