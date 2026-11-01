package middleware

import (
	"net/http"
	"slices"
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
		http.MethodOptions,
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
// It allows all origins, common HTTP methods, and standard headers, without credentials.
// To allow cookies or Authorization across origins, list the origins with CORSWithConfig.
func CORS() Middleware {
	return CORSWithConfig(defaultCORSConfig)
}

// CORSWithConfig returns a CORS middleware configured with the given CORSConfig.
// Parameters:
//   - cfg: custom CORS configuration (AllowOrigins, AllowMethods, AllowHeaders, AllowCredentials).
//
// It panics if AllowOrigins contains "*" while AllowCredentials is set, because that
// would let any website make logged-in requests on behalf of a user.
//
// Behavior:
//   - Sets `Vary: Origin`, so that caches keep responses for different origins apart.
//   - Sets `Access-Control-Allow-Origin` to "*", or to the request origin if it's listed.
//   - Sets `Access-Control-Allow-Credentials` only if cfg.AllowCredentials is set.
//   - Sets `Access-Control-Allow-Methods` and `Access-Control-Allow-Headers` on preflight.
//   - Handles OPTIONS preflight requests with HTTP 204 and stops further processing.
//   - Requests without an Origin header aren't cross-origin, and pass straight through.
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
	if cfg.AllowCredentials && slices.Contains(cfg.AllowOrigins, "*") {
		panic(`falcon: CORS can't combine AllowOrigins "*" with AllowCredentials; list the allowed origins instead`)
	}

	allowMethods := strings.Join(cfg.AllowMethods, ", ")
	allowHeaders := strings.Join(cfg.AllowHeaders, ", ")

	return func(next server.HandlerFunc) server.HandlerFunc {
		return func(c *server.Context) *server.Response {
			header := c.Writer.Header()
			header.Add("Vary", "Origin")

			origin := c.Request.Header.Get("Origin")
			if origin == "" {
				return next(c)
			}

			// Set Access-Control-Allow-Origin if matched
			allowed := false
			for _, o := range cfg.AllowOrigins {
				if o == "*" {
					header.Set("Access-Control-Allow-Origin", "*")
					allowed = true
					break
				}
				if strings.EqualFold(o, origin) {
					header.Set("Access-Control-Allow-Origin", origin)
					allowed = true
					break
				}
			}

			if allowed && cfg.AllowCredentials {
				header.Set("Access-Control-Allow-Credentials", "true")
			}

			// Continue normal middleware/handler flow
			if c.Request.Method != http.MethodOptions {
				return next(c)
			}

			// Handle preflight OPTIONS request
			if allowed {
				header.Set("Access-Control-Allow-Methods", allowMethods)
				header.Set("Access-Control-Allow-Headers", allowHeaders)
			}
			c.Writer.WriteHeader(http.StatusNoContent)
			c.Handled = true
			return &server.Response{Success: true, Message: "CORS preflight", Code: http.StatusNoContent}
		}
	}
}
