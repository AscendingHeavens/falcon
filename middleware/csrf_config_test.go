// Copyright 2026 Google LLC
// SPDX-License-Identifier: MIT

package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ascendingheavens/falcon/middleware"
	"github.com/ascendingheavens/falcon/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runCSRFConfigTest(
	t *testing.T,
	cfg middleware.CSRFConfig,
) *http.Cookie {
	t.Helper()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	ctx := &server.Context{
		Writer:  rec,
		Request: req,
	}

	next := func(c *server.Context) *server.Response {
		return &server.Response{
			Success: true,
			Message: "OK",
			Code:    http.StatusOK,
		}
	}

	resp := middleware.CSRFWithConfig(cfg)(next)(ctx)

	require.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.Code)

	cookies := rec.Result().Cookies()
	require.NotEmpty(t, cookies, "CSRF cookie should be created")

	return cookies[0]
}

func TestCSRFWithConfig_CookieDefaults(t *testing.T) {
	t.Run("Secret only uses secure defaults", func(t *testing.T) {
		cookie := runCSRFConfigTest(t, middleware.CSRFConfig{
			Secret: []byte("test-secret"),
		})

		assert.True(t, cookie.Secure)
		assert.True(t, cookie.HttpOnly)
		assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	})

	t.Run("Explicitly disable Secure", func(t *testing.T) {
		cookie := runCSRFConfigTest(t, middleware.CSRFConfig{
			Secret:          []byte("test-secret"),
			CookieSecure:    false,
			CookieSecureSet: true,
		})

		assert.False(t, cookie.Secure)
		assert.True(t, cookie.HttpOnly)
	})

	t.Run("Explicitly disable HttpOnly", func(t *testing.T) {
		cookie := runCSRFConfigTest(t, middleware.CSRFConfig{
			Secret:            []byte("test-secret"),
			CookieHTTPOnly:    false,
			CookieHTTPOnlySet: true,
		})

		assert.True(t, cookie.Secure)
		assert.False(t, cookie.HttpOnly)
	})

	t.Run("Explicitly disable both flags", func(t *testing.T) {
		cookie := runCSRFConfigTest(t, middleware.CSRFConfig{
			Secret:            []byte("test-secret"),
			CookieSecure:      false,
			CookieHTTPOnly:    false,
			CookieSecureSet:   true,
			CookieHTTPOnlySet: true,
		})

		assert.False(t, cookie.Secure)
		assert.False(t, cookie.HttpOnly)
	})

	t.Run("Explicitly enable both flags", func(t *testing.T) {
		cookie := runCSRFConfigTest(t, middleware.CSRFConfig{
			Secret:            []byte("test-secret"),
			CookieSecure:      true,
			CookieHTTPOnly:    true,
			CookieSecureSet:   true,
			CookieHTTPOnlySet: true,
		})

		assert.True(t, cookie.Secure)
		assert.True(t, cookie.HttpOnly)
	})
}
