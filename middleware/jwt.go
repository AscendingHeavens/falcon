package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ascendingheavens/falcon/server"
	"github.com/golang-jwt/jwt/v5"
)

// JWTMiddleware returns a middleware that validates JWT tokens in the Authorization header.
// The token must use the "Bearer " scheme, e.g. "Authorization: Bearer <token>".
// It verifies the token using the provided HMAC secret.
// If the token is invalid, missing, or has the wrong signing method, it returns a 401 Unauthorized response.
// On success, the token claims are stored in the Context under the key "user" for access in handlers.
//
// Example usage:
//
//	app.Use(JWTMiddleware("supersecretkey"))
func JWTMiddleware(secret string) Middleware {
	return func(next server.HandlerFunc) server.HandlerFunc {
		return func(c *server.Context) *server.Response {
			auth := c.Request.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				return c.ErrorJSON("Missing token", nil, http.StatusUnauthorized)
			}

			tokenStr := strings.TrimPrefix(auth, "Bearer ")

			// Parse and validate
			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
				// Optional: enforce signing method
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, errors.New("unexpected signing method")
				}
				return []byte(secret), nil
			})

			if err != nil || !token.Valid {
				return c.ErrorJSON("Invalid token", nil, http.StatusUnauthorized)
			}

			// Store claims in context for handlers
			if claims, ok := token.Claims.(jwt.MapClaims); ok {
				c.Set("user", claims)
			}

			return next(c)
		}
	}
}
