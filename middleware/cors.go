package middleware

import (
	"net/http"
	"strings"

	"github.com/ascendingheavens/falcon/server"
)

// defaultCORSConfig defines a permissive default CORS configuration.
var defaultCORSConfig = CORSConfig{
	AllowOrigins: []string{"*"},
	AllowMethods: []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPut,
		http.MethodPatch,
		http.MethodPost,
		http.MethodDelete,
	},
	AllowHeaders: []string{
		"Content-Type",
		"Authorization",
		"Accept",
		"Origin",
		"X-Requested-With",
	},
}

// CORS returns a default CORS middleware using defaultCORSConfig.
// It allows all origins, common HTTP methods, and standard headers.
func CORS() Middleware {
	return CORSWithConfig(defaultCORSConfig)
}

// CORSWithConfig returns a CORS middleware configured with the given CORSConfig.
// Parameters:
//   - cfg: custom CORS configuration (AllowOrigins, AllowMethods, AllowHeaders).
//
// Behavior:
//   - Sets `Access-Control-Allow-Origin` to the request origin if allowed.
//   - Sets `Access-Control-Allow-Methods` and `Access-Control-Allow-Headers`.
//   - Sets `Access-Control-Allow-Credentials` to true.
//   - Handles OPTIONS preflight requests with HTTP 204 and stops further processing.
func CORSWithConfig(cfg CORSConfig) Middleware {
	// Set defaults if empty
	if len(cfg.AllowOrigins) == 0 {
		cfg.AllowOrigins = []string{"*"}
	}
	if len(cfg.AllowMethods) == 0 {
		cfg.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	}
	if len(cfg.AllowHeaders) == 0 {
		cfg.AllowHeaders = []string{"Content-Type", "Authorization"}
	}

	return func(next server.HandlerFunc) server.HandlerFunc {
		return func(c *server.Context) *server.Response {
			origin := c.Request.Header.Get("Origin")

			// Set Access-Control-Allow-Origin if matched
			if origin != "" {
				for _, o := range cfg.AllowOrigins {
					if o == "*" || strings.EqualFold(o, origin) {
						c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
						break
					}
				}
			}

			// Set other CORS headers
			c.Writer.Header().Set("Access-Control-Allow-Methods", strings.Join(cfg.AllowMethods, ", "))
			c.Writer.Header().Set("Access-Control-Allow-Headers", strings.Join(cfg.AllowHeaders, ", "))
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")

			// Handle preflight OPTIONS request
			if c.Request.Method == http.MethodOptions {
				c.Writer.WriteHeader(http.StatusNoContent)
				c.Handled = true
				return &server.Response{Success: true, Message: "CORS preflight", Code: http.StatusNoContent}
			}

			// Continue normal middleware/handler flow
			return next(c)
		}
	}
}
