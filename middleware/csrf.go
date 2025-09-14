package middleware

import (
	"net/http"
	"time"

	"github.com/ascendingheavens/falcon/server"
)

var defaultCSRFConfig = CSRFConfig{
	TokenHeader:    "X-CSRF-Token",
	TokenCookie:    "csrf_token",
	ContextKey:     "csrf_token",
	Expiry:         24 * time.Hour,
	Secret:         []byte("supersecretkey"),
	SkipMethods:    []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace},
	CookieSecure:   true,
	CookieHTTPOnly: true,
}

// CSRF returns a middleware using the default CSRF configuration.
// It automatically validates incoming requests for unsafe methods
// and sets a CSRF token in the context and cookie.
func CSRF() Middleware {
	return CSRFWithConfig(defaultCSRFConfig)
}

// CSRFWithConfig returns a CSRF middleware with a custom configuration.
//
// Parameters:
//   - cfg: CSRFConfig struct to override defaults (header, cookie, secret, expiry, etc.)
//
// Behavior:
//   - Skips validation for HTTP methods listed in cfg.SkipMethods.
//   - Reads token from request header or cookie.
//   - Validates the token against the server-side secret.
//   - Returns 403 Forbidden if validation fails (or calls cfg.ErrorHandler if defined).
//   - Ensures a valid token is set in context and cookie for the client.
func CSRFWithConfig(cfg CSRFConfig) Middleware {
	return func(next server.HandlerFunc) server.HandlerFunc {
		return func(c *server.Context) *server.Response {
			// Skip safe methods
			for _, m := range cfg.SkipMethods {
				if c.Request.Method == m {
					return next(c)
				}
			}

			// Read token from header or cookie
			clientToken := c.Request.Header.Get(cfg.TokenHeader)
			if clientToken == "" {
				if cookie, err := c.Request.Cookie(cfg.TokenCookie); err == nil {
					clientToken = cookie.Value
				}
			}

			// Validate token
			if clientToken != "" {
				serverToken := getOrCreateCSRFToken(c, cfg)
				if !validateCSRFToken(cfg.Secret, serverToken, clientToken) {
					if cfg.ErrorHandler != nil {
						return cfg.ErrorHandler(c, ErrCSRFInvalid)
					}
					return c.String(http.StatusForbidden, ErrCSRFInvalid.Error())
				}
			}

			// Ensure token exists in context and cookie
			token := getOrCreateCSRFToken(c, cfg)
			http.SetCookie(c.Writer, &http.Cookie{
				Name:     cfg.TokenCookie,
				Value:    token,
				Path:     "/",
				Expires:  time.Now().Add(cfg.Expiry),
				Secure:   cfg.CookieSecure,
				HttpOnly: cfg.CookieHTTPOnly,
			})
			c.Params[cfg.ContextKey] = token

			return next(c)
		}
	}
}
