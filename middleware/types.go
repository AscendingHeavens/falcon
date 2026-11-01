package middleware

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ascendingheavens/falcon/server"
)

// Middleware defines the function signature for all middleware in Falcon.
// A middleware wraps a HandlerFunc, allowing pre- or post-processing of requests.
// Examples include logging, authentication, profiling, or panic recovery.
type Middleware func(server.HandlerFunc) server.HandlerFunc

// ConditionalMiddleware pairs a middleware with path patterns, which decide the routes it
// applies to. Patterns are compared with a route's path as registered, one segment at a time:
//   - "/api/*" matches "/api", "/api/users" and "/api/users/:id", but not "/apiary";
//   - "/*" matches every route;
//   - "/users/:id" with no wildcard matches only the route with exactly that path.
type ConditionalMiddleware struct {
	Patterns   []string   // The path patterns to match, e.g., "/api/v1/*"
	Negate     bool       // Apply the middleware to routes matching none of the patterns instead
	Middleware Middleware // The middleware function to apply
}

// NewConditionalMiddleware creates a ConditionalMiddleware. It panics on patterns that
// couldn't match what they appear to mean, so the mistake shows up at startup.
func NewConditionalMiddleware(mw Middleware, negate bool, patterns []string) ConditionalMiddleware {
	if len(patterns) == 0 {
		panic("falcon: conditional middleware needs at least one path pattern")
	}
	for _, p := range patterns {
		if !strings.HasPrefix(p, "/") {
			panic(fmt.Sprintf("falcon: middleware pattern %q must start with \"/\"", p))
		}
		if strings.HasSuffix(p, "*") && !strings.HasSuffix(p, "/*") {
			panic(fmt.Sprintf("falcon: middleware pattern %q matches whole segments only; did you mean %q?",
				p, strings.TrimSuffix(p, "*")+"/*"))
		}
	}
	return ConditionalMiddleware{Patterns: patterns, Negate: negate, Middleware: mw}
}

// AppliesTo reports whether the middleware should wrap the route with this path.
func (cm ConditionalMiddleware) AppliesTo(routePath string) bool {
	return slices.ContainsFunc(cm.Patterns, func(p string) bool {
		return patternMatches(p, routePath)
	}) != cm.Negate
}

// patternMatches compares a pattern with a route's path, by whole segments.
func patternMatches(pattern, routePath string) bool {
	base, wildcard := strings.CutSuffix(pattern, "/*")
	if !wildcard {
		return pattern == routePath
	}
	return routePath == base || strings.HasPrefix(routePath, base+"/")
}

// CORSConfig defines configuration for Cross-Origin Resource Sharing (CORS).
// Allows specifying which origins, headers, and methods are permitted.
type CORSConfig struct {
	AllowOrigins     []string // Allowed origins, e.g., ["https://example.com"] or ["*"]
	AllowMethods     []string // Allowed HTTP methods, e.g., ["GET", "POST"]
	AllowHeaders     []string // Allowed headers, e.g., ["Content-Type", "Authorization"]
	AllowCredentials bool     // Allow cookies and Authorization across origins. Can't be combined with "*"
}

// CSRFConfig defines configuration for CSRF protection middleware.
type CSRFConfig struct {
	TokenHeader    string                                        // Header the client can send the token in
	TokenForm      string                                        // Form field the client can send the token in, for HTML forms
	TokenCookie    string                                        // Cookie name holding the value the token is derived from
	ContextKey     string                                        // Context key where handlers read the token to give to the client
	Expiry         time.Duration                                 // Cookie expiration duration
	Secret         []byte                                        // HMAC secret. If empty a random one is generated, so set it when running several instances
	SkipMethods    []string                                      // HTTP methods that don't require validation (safe methods)
	ErrorHandler   func(*server.Context, error) *server.Response // Optional custom error handler
	CookieSecure   bool                                          // Whether the cookie is Secure
	CookieHTTPOnly bool                                          // Whether the cookie is HttpOnly
	CookieSameSite http.SameSite                                 // The cookie's SameSite mode. Defaults to Lax
}
