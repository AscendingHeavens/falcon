package falcon

import (
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ascendingheavens/falcon/middleware"
	"github.com/stretchr/testify/assert"
)

func doFrom(s *Server, remoteAddr, method, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestRateLimit_ThroughServeHTTP(t *testing.T) {
	s := New()
	s.Use(middleware.RateLimit(2, time.Minute))
	s.GET("/x", ok)

	assert.Equal(t, 200, doFrom(s, "1.1.1.1:1", "GET", "/x").Code)
	assert.Equal(t, 200, doFrom(s, "1.1.1.1:1", "GET", "/x").Code)

	rec := doFrom(s, "1.1.1.1:1", "GET", "/x")
	assert.Equal(t, 429, rec.Code)
	assert.JSONEq(t, `{"success":false,"message":"Too Many Requests","code":429}`, rec.Body.String())
	assert.Equal(t, "30", rec.Header().Get("Retry-After"))

	assert.Equal(t, 429, doFrom(s, "1.1.1.1:1", "GET", "/nope").Code,
		"a global limit also covers paths that don't exist, so probing for them counts")
}

func TestRateLimit_PerRouteLimitsAreSeparate(t *testing.T) {
	s := New()
	s.UseIf(middleware.RateLimit(1, time.Minute), "/auth/login")
	s.UseIf(middleware.RateLimit(3, time.Minute), "/api/*")
	s.POST("/auth/login", ok)
	s.GET("/api/a", ok)
	s.GET("/api/b", ok)
	s.GET("/free", ok)

	assert.Equal(t, 200, doFrom(s, "1.1.1.1:1", "POST", "/auth/login").Code)
	assert.Equal(t, 429, doFrom(s, "1.1.1.1:1", "POST", "/auth/login").Code)

	// Being locked out of login doesn't touch the API's own limit, which /api/a and /api/b share.
	assert.Equal(t, 200, doFrom(s, "1.1.1.1:1", "GET", "/api/a").Code)
	assert.Equal(t, 200, doFrom(s, "1.1.1.1:1", "GET", "/api/b").Code)
	assert.Equal(t, 200, doFrom(s, "1.1.1.1:1", "GET", "/api/a").Code)
	assert.Equal(t, 429, doFrom(s, "1.1.1.1:1", "GET", "/api/b").Code)

	for range 5 {
		assert.Equal(t, 200, doFrom(s, "1.1.1.1:1", "GET", "/free").Code)
	}
}

func TestRateLimit_ErrorMapperCanChangeTheResponse(t *testing.T) {
	s := New(WithErrorMapper(func(err error) *Response {
		if errors.Is(err, middleware.ErrRateLimited) {
			return &Response{Message: "Slow down", Details: map[string]string{"docs": "/limits"}, Code: 429}
		}
		return nil
	}))
	s.Use(middleware.RateLimit(1, time.Minute))
	s.GET("/x", ok)

	doFrom(s, "1.1.1.1:1", "GET", "/x")
	rec := doFrom(s, "1.1.1.1:1", "GET", "/x")

	assert.Equal(t, 429, rec.Code)
	assert.JSONEq(t, `{"success":false,"message":"Slow down","details":{"docs":"/limits"},"code":429}`, rec.Body.String())
	assert.NotEmpty(t, rec.Header().Get("Retry-After"), "the header survives a mapped response")
}

func TestConcurrencyLimit_ThroughServeHTTP(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	s := New()
	s.UseIf(middleware.ConcurrencyLimit(1), "/report")
	s.GET("/report", func(c *Context) *Response {
		close(started)
		<-release
		return c.OK("done", nil)
	})
	s.GET("/other", ok)

	done := make(chan int)
	go func() { done <- do(s, "GET", "/report").Code }()
	<-started

	busy := do(s, "GET", "/report")
	assert.Equal(t, 503, busy.Code)
	assert.Equal(t, "application/json", busy.Header().Get("Content-Type"))
	assert.Equal(t, 200, do(s, "GET", "/other").Code, "other routes aren't limited")

	close(release)
	assert.Equal(t, 200, <-done)
}
