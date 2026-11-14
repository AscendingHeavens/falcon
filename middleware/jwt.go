// Copyright 2026 Google LLC
// SPDX-License-Identifier: MIT

package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ascendingheavens/falcon/server"
	"github.com/golang-jwt/jwt/v5"
)

// JWTMiddleware returns a middleware that validates JWT tokens in the Authorization header.
// Tokens must use the Bearer scheme and contain a valid expiration claim.
// It verifies the token using the provided HMAC secret.
// On success, claims are stored in the Context under the key "user".
func JWTMiddleware(secret string) Middleware {
	if secret == "" {
		panic("JWTMiddleware: secret must not be empty")
	}

	return func(next server.HandlerFunc) server.HandlerFunc {
		return func(c *server.Context) *server.Response {
			auth := c.Request.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				return c.ErrorJSON("Missing token", nil, http.StatusUnauthorized)
			}

			tokenStr := strings.TrimPrefix(auth, "Bearer ")

			token, err := jwt.Parse(
				tokenStr,
				func(t *jwt.Token) (interface{}, error) {
					if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, errors.New("unexpected signing method")
					}

					return []byte(secret), nil
				},
				jwt.WithExpirationRequired(),
			)

			if err != nil || !token.Valid {
				return c.ErrorJSON("Invalid token", nil, http.StatusUnauthorized)
			}

			if claims, ok := token.Claims.(jwt.MapClaims); ok {
				c.Set("user", claims)
			}

			return next(c)
		}
	}
}
