package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ascendingheavens/falcon/server"
	"github.com/stretchr/testify/assert"
)

func newTestContext(method string) *server.Context {
	req := httptest.NewRequest(method, "/", nil)
	w := httptest.NewRecorder()
	return &server.Context{
		Request: req,
		Writer:  w,
	}
}

func TestLogger_CallsNextAndReturnsResponse(t *testing.T) {
	called := false
	c := newTestContext(http.MethodGet)
	m := Logger()

	handler := m(func(ctx *server.Context) *server.Response {
		called = true
		return &server.Response{Success: true, Code: http.StatusOK}
	})

	resp := handler(c)

	assert.True(t, called, "logger must call next handler")
	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestRecovery_NoPanic_PassesThrough(t *testing.T) {
	called := false
	c := newTestContext(http.MethodGet)
	m := Recovery()

	handler := m(func(ctx *server.Context) *server.Response {
		called = true
		return &server.Response{Success: true, Code: http.StatusOK}
	})

	resp := handler(c)

	assert.True(t, called, "recovery must call next handler when no panic")
	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestRecovery_Panic_Returns500_JSON(t *testing.T) {
	c := newTestContext(http.MethodGet)
	m := Recovery()

	handler := m(func(ctx *server.Context) *server.Response {
		panic("boom")
	})

	// This will trigger recovery middleware
	resp := handler(c)

	assert.False(t, resp.Success)
	assert.Equal(t, http.StatusInternalServerError, resp.Code)

	// Now check what the client gets
	c.WriteResponse(resp)
	rec := c.Writer.(*httptest.ResponseRecorder)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Internal Server Error")
	assert.NotContains(t, body, "boom", "the panic message must not reach the client")
}

func TestRecovery_Panic_IsAnEnvelopeEvenForBrowsers(t *testing.T) {
	c := newTestContext(http.MethodGet)
	c.Request.Header.Set("Accept", "text/html")
	m := Recovery()

	handler := m(func(ctx *server.Context) *server.Response {
		panic("boom")
	})

	c.WriteResponse(handler(c))

	rec := c.Writer.(*httptest.ResponseRecorder)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

func TestRecovery_AbortHandlerPassesThrough(t *testing.T) {
	c := newTestContext(http.MethodGet)
	handler := Recovery()(func(ctx *server.Context) *server.Response {
		panic(http.ErrAbortHandler)
	})

	assert.PanicsWithValue(t, http.ErrAbortHandler, func() { handler(c) })
}

func TestConditionalMiddleware_AppliesTo(t *testing.T) {
	tests := []struct {
		pattern string
		route   string
		want    bool
	}{
		{"/api/*", "/api", true},
		{"/api/*", "/api/", true},
		{"/api/*", "/api/users", true},
		{"/api/*", "/api/users/:id", true},
		{"/api/*", "/apiary", false},
		{"/api/*", "/ap", false},
		{"/api/*", "/other/api", false},
		{"/*", "/", true},
		{"/*", "/anything/at/all", true},
		{"/health", "/health", true},
		{"/health", "/healthy", false},
		{"/health", "/health/deep", false},
		{"/health", "/health/", false},
		{"/users/:id", "/users/:id", true},
		{"/users/:id", "/users/:name", false},
		{"/static/*filepath", "/static/*filepath", true},
		{"/", "/", true},
		{"/", "/x", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+" on "+tt.route, func(t *testing.T) {
			noop := func(next server.HandlerFunc) server.HandlerFunc { return next }

			useIf := NewConditionalMiddleware(noop, false, []string{tt.pattern})
			assert.Equal(t, tt.want, useIf.AppliesTo(tt.route))

			useIfNot := NewConditionalMiddleware(noop, true, []string{tt.pattern})
			assert.Equal(t, !tt.want, useIfNot.AppliesTo(tt.route))
		})
	}

	t.Run("any of several patterns", func(t *testing.T) {
		noop := func(next server.HandlerFunc) server.HandlerFunc { return next }
		cm := NewConditionalMiddleware(noop, false, []string{"/health", "/auth/*"})
		assert.True(t, cm.AppliesTo("/health"))
		assert.True(t, cm.AppliesTo("/auth/login"))
		assert.False(t, cm.AppliesTo("/users"))
	})
}

func TestConditionalMiddleware_MalformedPatternsPanic(t *testing.T) {
	noop := func(next server.HandlerFunc) server.HandlerFunc { return next }

	assert.Panics(t, func() { NewConditionalMiddleware(noop, false, nil) }, "no patterns")
	assert.Panics(t, func() { NewConditionalMiddleware(noop, false, []string{"api/*"}) }, "no leading slash")
	assert.Panics(t, func() { NewConditionalMiddleware(noop, false, []string{"*"}) }, "bare wildcard")
	assert.PanicsWithValue(t,
		`falcon: middleware pattern "/api*" matches whole segments only; did you mean "/api/*"?`,
		func() { NewConditionalMiddleware(noop, false, []string{"/api*"}) })
}

func TestProfilingMiddleware_CallsNextAndReturnsResponse(t *testing.T) {
	called := false
	c := newTestContext(http.MethodGet)
	m := ProfilingMiddleware()

	handler := m(func(ctx *server.Context) *server.Response {
		called = true
		return &server.Response{Success: true, Code: http.StatusCreated}
	})

	resp := handler(c)

	assert.True(t, called, "profiling middleware must call next handler")
	assert.Equal(t, http.StatusCreated, resp.Code)
}

func TestRecovery_Panic_WhenAlreadyHandled_DoesNotWriteAgain(t *testing.T) {
	c := newTestContext(http.MethodGet)
	c.Handled = true // simulate handler already wrote a response
	m := Recovery()

	handler := m(func(ctx *server.Context) *server.Response {
		panic("already handled panic")
	})

	resp := handler(c)
	assert.NotNil(t, resp, "outer middleware must always get a Response")
	c.WriteResponse(resp)

	rec := c.Writer.(*httptest.ResponseRecorder)
	// Should NOT write anything new, response should remain empty
	assert.Equal(t, 200, rec.Code) // Recorder defaults to 200 if not written
	assert.Empty(t, rec.Body.String())
}
