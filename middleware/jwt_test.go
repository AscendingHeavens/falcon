package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ascendingheavens/falcon/middleware"
	"github.com/ascendingheavens/falcon/server"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
)

func TestJWTMiddleware(t *testing.T) {
	secret := "mysecret"

	// Dummy next handler
	nextHandler := func(c *server.Context) *server.Response {
		return &server.Response{Success: true, Message: "OK", Code: 200}
	}

	t.Run("Missing Authorization header", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		c := &server.Context{Writer: rec, Request: req}

		mw := middleware.JWTMiddleware(secret)
		resp := mw(nextHandler)(c)

		assert.False(t, resp.Success)
		assert.Equal(t, http.StatusUnauthorized, resp.Code)
		assert.True(t, c.Handled)
	})

	t.Run("Invalid token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Bearer invalidtoken")
		c := &server.Context{Writer: rec, Request: req}

		mw := middleware.JWTMiddleware(secret)
		resp := mw(nextHandler)(c)

		assert.False(t, resp.Success)
		assert.Equal(t, http.StatusUnauthorized, resp.Code)
		assert.True(t, c.Handled)
	})

	t.Run("Valid token sets claims and calls next", func(t *testing.T) {
		// Create a valid token
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"email": "test@example.com",
			"exp":   time.Now().Add(time.Hour).Unix(),
		})
		tokenStr, err := token.SignedString([]byte(secret))
		assert.NoError(t, err)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		c := &server.Context{Writer: rec, Request: req}

		mw := middleware.JWTMiddleware(secret)
		resp := mw(nextHandler)(c)

		assert.True(t, resp.Success)
		assert.Equal(t, 200, resp.Code)

		claims := c.Get("user").(jwt.MapClaims)
		assert.Equal(t, "test@example.com", claims["email"])
	})
}
