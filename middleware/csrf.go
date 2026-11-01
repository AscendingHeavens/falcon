package middleware

import (
	"net/http"
	"slices"
	"time"

	"github.com/ascendingheavens/falcon/server"
)

var defaultCSRFConfig = CSRFConfig{
	TokenHeader:    "X-CSRF-Token",
	TokenCookie:    "csrf_token",
	ContextKey:     "csrf_token",
	Expiry:         24 * time.Hour,
	SkipMethods:    []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace},
	CookieSecure:   true,
	CookieHTTPOnly: true,
	CookieSameSite: http.SameSiteLaxMode,
}

// CSRF returns a middleware using the default CSRF configuration.
// It validates incoming requests for unsafe methods and gives every request a token.
//
// The secret is random, so tokens stop working on restart and aren't shared between
// instances. Use CSRFWithConfig with a Secret if that matters.
func CSRF() Middleware {
	return CSRFWithConfig(defaultCSRFConfig)
}

// CSRFWithConfig returns a CSRF middleware with a custom configuration.
//
// Parameters:
//   - cfg: CSRFConfig struct to override defaults (header, cookie, secret, expiry, etc.)
//
// It works as a signed double submit: a cookie holds a random value, and an unsafe
// request must send the HMAC of that value in cfg.TokenHeader. Another site can make
// a browser send the cookie, but it can't read it or compute the HMAC to send the header.
//
// Handlers read the token to give to the client with c.Get(cfg.ContextKey), for example
// from an endpoint the frontend calls first, or by rendering it into a page.
//
// Behavior:
//   - Skips validation for HTTP methods listed in cfg.SkipMethods.
//   - Reads the token from the request header only: a browser attaches cookies to
//     forged requests on its own, so a cookie can never prove anything by itself.
//   - Validates the token against the cookie and the server-side secret.
//   - Returns 403 Forbidden if the token or cookie is missing or the token is wrong
//     (or calls cfg.ErrorHandler if defined).
//   - Ensures a cookie is set and a token is in the context on every other request.
func CSRFWithConfig(cfg CSRFConfig) Middleware {
	// Set defaults if empty
	if cfg.TokenHeader == "" {
		cfg.TokenHeader = defaultCSRFConfig.TokenHeader
	}
	if cfg.TokenCookie == "" {
		cfg.TokenCookie = defaultCSRFConfig.TokenCookie
	}
	if cfg.ContextKey == "" {
		cfg.ContextKey = defaultCSRFConfig.ContextKey
	}
	if cfg.Expiry == 0 {
		cfg.Expiry = defaultCSRFConfig.Expiry
	}
	if cfg.SkipMethods == nil {
		cfg.SkipMethods = defaultCSRFConfig.SkipMethods
	}
	if cfg.CookieSameSite == 0 {
		cfg.CookieSameSite = defaultCSRFConfig.CookieSameSite
	}
	if len(cfg.Secret) == 0 {
		cfg.Secret = []byte(generateCSRFToken(32))
	}

	return func(next server.HandlerFunc) server.HandlerFunc {
		return func(c *server.Context) *server.Response {
			serverToken := readCSRFCookie(c, cfg)

			if !slices.Contains(cfg.SkipMethods, c.Request.Method) {
				clientToken := c.Request.Header.Get(cfg.TokenHeader)
				if serverToken == "" || !validateCSRFToken(cfg.Secret, serverToken, clientToken) {
					if cfg.ErrorHandler != nil {
						return cfg.ErrorHandler(c, ErrCSRFInvalid)
					}
					return c.ErrorJSON(ErrCSRFInvalid.Error(), nil, http.StatusForbidden)
				}
			}

			// Ensure token exists in context and cookie
			if serverToken == "" {
				serverToken = generateCSRFToken(32)
			}
			http.SetCookie(c.Writer, &http.Cookie{
				Name:     cfg.TokenCookie,
				Value:    serverToken,
				Path:     "/",
				Expires:  time.Now().Add(cfg.Expiry),
				Secure:   cfg.CookieSecure,
				HttpOnly: cfg.CookieHTTPOnly,
				SameSite: cfg.CookieSameSite,
			})
			c.Set(cfg.ContextKey, signCSRFToken(cfg.Secret, serverToken))

			return next(c)
		}
	}
}
