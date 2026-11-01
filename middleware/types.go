package middleware

import (
	"net/http"
	"time"

	"github.com/ascendingheavens/falcon/server"
)

// Middleware defines the function signature for all middleware in Falcon.
// A middleware wraps a HandlerFunc, allowing pre- or post-processing of requests.
// Examples include logging, authentication, profiling, or panic recovery.
type Middleware func(server.HandlerFunc) server.HandlerFunc

// ConditionalMiddleware pairs a middleware with a path pattern.
// The middleware is only applied if the request path matches the pattern.
// Patterns can include a wildcard '*' at the end to match any subpath.
type ConditionalMiddleware struct {
	Pattern    string     // The URL path pattern to match, e.g., "/api/v1/*"
	Middleware Middleware // The middleware function to apply when the pattern matches
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
	TokenHeader    string                                        // Header the client must send the token in
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
