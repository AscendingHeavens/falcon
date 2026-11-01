package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ascendingheavens/falcon/server"
	"github.com/stretchr/testify/assert"
)

func newCorsTestContext(method, origin string) *server.Context {
	req := httptest.NewRequest(method, "/", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	return &server.Context{
		Request: req,
		Writer:  w,
	}
}

func TestCORS_DefaultConfig_AllowsOrigin(t *testing.T) {
	called := false
	c := newCorsTestContext(http.MethodGet, "http://example.com")
	m := CORS()

	handler := m(func(ctx *server.Context) *server.Response {
		called = true
		return &server.Response{Success: true, Code: http.StatusOK}
	})

	resp := handler(c)

	assert.True(t, called, "next handler should be called for non-OPTIONS request")
	assert.Equal(t, http.StatusOK, resp.Code)

	headers := c.Writer.Header()
	assert.Equal(t, "*", headers.Get("Access-Control-Allow-Origin"), "a wildcard must never echo the caller's origin")
	assert.Empty(t, headers.Get("Access-Control-Allow-Credentials"), "credentials must be off unless asked for")
	assert.Equal(t, "Origin", headers.Get("Vary"))
}

func TestCORS_PreflightRequest_SetsNoContentAndStopsChain(t *testing.T) {
	called := false
	c := newCorsTestContext(http.MethodOptions, "http://example.com")
	m := CORS()

	handler := m(func(ctx *server.Context) *server.Response {
		called = true
		return &server.Response{Success: true, Code: http.StatusOK}
	})

	resp := handler(c)

	assert.False(t, called, "next handler should NOT be called for preflight requests")
	assert.Equal(t, http.StatusNoContent, resp.Code)

	// FIX: type assert to *httptest.ResponseRecorder
	recorder, ok := c.Writer.(*httptest.ResponseRecorder)
	assert.True(t, ok, "c.Writer must be a *httptest.ResponseRecorder in tests")
	assert.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestCORS_CustomConfig_SpecificOriginAllowed(t *testing.T) {
	cfg := CORSConfig{
		AllowOrigins: []string{"https://allowed.com"},
		AllowMethods: []string{http.MethodGet, http.MethodPost},
		AllowHeaders: []string{"X-Custom-Header"},
	}
	m := CORSWithConfig(cfg)

	called := false
	c := newCorsTestContext(http.MethodGet, "https://allowed.com")

	handler := m(func(ctx *server.Context) *server.Response {
		called = true
		return &server.Response{Success: true, Code: http.StatusOK}
	})

	handler(c)

	headers := c.Writer.Header()
	assert.Equal(t, "https://allowed.com", headers.Get("Access-Control-Allow-Origin"))
	assert.Empty(t, headers.Get("Access-Control-Allow-Methods"), "only a preflight needs the methods")
	assert.True(t, called)

	preflight := newCorsTestContext(http.MethodOptions, "https://allowed.com")
	handler(preflight)

	headers = preflight.Writer.Header()
	assert.Equal(t, "https://allowed.com", headers.Get("Access-Control-Allow-Origin"))
	assert.Equal(t, strings.Join(cfg.AllowMethods, ", "), headers.Get("Access-Control-Allow-Methods"))
	assert.Equal(t, strings.Join(cfg.AllowHeaders, ", "), headers.Get("Access-Control-Allow-Headers"))
}

func TestCORS_Credentials_OnlyForListedOriginsThatAskForThem(t *testing.T) {
	m := CORSWithConfig(CORSConfig{
		AllowOrigins:     []string{"https://allowed.com"},
		AllowCredentials: true,
	})
	handler := m(func(ctx *server.Context) *server.Response {
		return &server.Response{Success: true, Code: http.StatusOK}
	})

	allowed := newCorsTestContext(http.MethodGet, "https://allowed.com")
	handler(allowed)
	assert.Equal(t, "https://allowed.com", allowed.Writer.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", allowed.Writer.Header().Get("Access-Control-Allow-Credentials"))

	// The attack this guards against: a malicious site reading a logged-in user's data.
	evil := newCorsTestContext(http.MethodGet, "https://evil.com")
	handler(evil)
	assert.Empty(t, evil.Writer.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, evil.Writer.Header().Get("Access-Control-Allow-Credentials"))

	evilPreflight := newCorsTestContext(http.MethodOptions, "https://evil.com")
	handler(evilPreflight)
	assert.Empty(t, evilPreflight.Writer.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, evilPreflight.Writer.Header().Get("Access-Control-Allow-Credentials"))
	assert.Empty(t, evilPreflight.Writer.Header().Get("Access-Control-Allow-Methods"))
}

func TestCORS_WildcardWithCredentialsPanics(t *testing.T) {
	assert.Panics(t, func() {
		CORSWithConfig(CORSConfig{AllowOrigins: []string{"*"}, AllowCredentials: true})
	})
	assert.Panics(t, func() {
		CORSWithConfig(CORSConfig{AllowCredentials: true}) // AllowOrigins defaults to "*"
	})
}

func TestCORS_NoOriginPassesThrough(t *testing.T) {
	called := false
	c := newCorsTestContext(http.MethodOptions, "")
	handler := CORS()(func(ctx *server.Context) *server.Response {
		called = true
		return &server.Response{Success: true, Code: http.StatusOK}
	})

	resp := handler(c)

	assert.True(t, called, "without an Origin it isn't a preflight, so a registered OPTIONS route still runs")
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Empty(t, c.Writer.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Origin", c.Writer.Header().Get("Vary"))
}

func TestCORS_CustomConfig_OriginNotAllowed(t *testing.T) {
	cfg := CORSConfig{
		AllowOrigins: []string{"https://allowed.com"},
	}
	m := CORSWithConfig(cfg)

	c := newCorsTestContext(http.MethodGet, "https://disallowed.com")

	handler := m(func(ctx *server.Context) *server.Response {
		return &server.Response{Success: true, Code: http.StatusOK}
	})

	handler(c)

	headers := c.Writer.Header()
	// Should not echo back disallowed origin
	assert.Empty(t, headers.Get("Access-Control-Allow-Origin"))
}
