package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ascendingheavens/falcon/server"
	"github.com/golang-jwt/jwt/v5"
)

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
