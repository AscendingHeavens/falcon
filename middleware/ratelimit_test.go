package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ascendingheavens/falcon/server"
	"github.com/stretchr/testify/assert"
)

// fakeClock lets tests move time forward instead of sleeping.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestRateLimiter_AllowsTheBurstThenRefuses(t *testing.T) {
	clock := newFakeClock()
	l := newRateLimiter(RateLimitConfig{Limit: 3, Per: 3 * time.Second}, clock.Now)

	for i := range 3 {
		r := l.take("a")
		assert.True(t, r.allowed, "request %d", i+1)
		assert.Equal(t, 2-i, r.remaining)
	}

	r := l.take("a")
	assert.False(t, r.allowed)
	assert.Equal(t, 0, r.remaining)
	assert.Equal(t, time.Second, r.retryAfter, "one token comes back every second")
	assert.Equal(t, 3*time.Second, r.reset)
}

func TestRateLimiter_Refills(t *testing.T) {
	clock := newFakeClock()
	l := newRateLimiter(RateLimitConfig{Limit: 2, Per: time.Second}, clock.Now)
	l.take("a")
	l.take("a")
	assert.False(t, l.take("a").allowed)

	clock.Advance(400 * time.Millisecond) // 0.8 tokens: not enough
	r := l.take("a")
	assert.False(t, r.allowed)
	assert.Equal(t, 100*time.Millisecond, r.retryAfter.Round(time.Millisecond))

	clock.Advance(100 * time.Millisecond) // 1 token
	assert.True(t, l.take("a").allowed)
	assert.False(t, l.take("a").allowed)

	clock.Advance(time.Hour) // Never more than the burst, however long it waits
	assert.True(t, l.take("a").allowed)
	assert.True(t, l.take("a").allowed)
	assert.False(t, l.take("a").allowed)
}

// A wait shorter than the cleanup interval, so the bucket survives and the cap is what counts.
func TestRateLimiter_NeverRefillsBeyondTheBurst(t *testing.T) {
	clock := newFakeClock()
	l := newRateLimiter(RateLimitConfig{Limit: 10, Per: 10 * time.Second}, clock.Now)
	l.take("a") // 9 left

	clock.Advance(5 * time.Second) // 9 + 5 would be 14 without the cap

	allowed := 0
	for range 20 {
		if l.take("a").allowed {
			allowed++
		}
	}
	assert.Equal(t, 10, allowed)
}

func TestRateLimiter_ClientsAreIndependent(t *testing.T) {
	l := newRateLimiter(RateLimitConfig{Limit: 1, Per: time.Minute}, newFakeClock().Now)

	assert.True(t, l.take("a").allowed)
	assert.False(t, l.take("a").allowed)
	assert.True(t, l.take("b").allowed, "a busy client must not use up another's limit")
}

func TestRateLimiter_CustomBurst(t *testing.T) {
	clock := newFakeClock()
	l := newRateLimiter(RateLimitConfig{Limit: 60, Per: time.Minute, Burst: 2}, clock.Now)

	assert.True(t, l.take("a").allowed)
	assert.True(t, l.take("a").allowed)
	assert.False(t, l.take("a").allowed, "60 a minute, but only 2 at once")

	clock.Advance(time.Second)
	assert.True(t, l.take("a").allowed)
}

func TestRateLimiter_InvalidConfigPanics(t *testing.T) {
	for name, cfg := range map[string]RateLimitConfig{
		"no limit":       {Per: time.Second},
		"negative limit": {Limit: -1, Per: time.Second},
		"no period":      {Limit: 1},
		"negative burst": {Limit: 1, Per: time.Second, Burst: -1},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Panics(t, func() { RateLimitWithConfig(cfg) })
		})
	}
	assert.Panics(t, func() { ConcurrencyLimit(0) })
}

func bucketCount(l *rateLimiter) int {
	n := 0
	for i := range l.shards {
		l.shards[i].mu.Lock()
		n += len(l.shards[i].buckets)
		l.shards[i].mu.Unlock()
	}
	return n
}

// Without cleanup, a client cycling through addresses would grow memory forever.
func TestRateLimiter_ForgetsClientsThatHaveRefilled(t *testing.T) {
	clock := newFakeClock()
	l := newRateLimiter(RateLimitConfig{Limit: 10, Per: 10 * time.Second}, clock.Now)

	for i := range 1000 {
		l.take(fmt.Sprintf("client-%d", i))
	}
	assert.Equal(t, 1000, bucketCount(l))

	l.take("busy")
	l.take("busy")

	// Long enough for the 1000 to refill. "busy" spent 2 tokens just now, so it hasn't.
	clock.Advance(10 * time.Second)
	l.take("busy")
	l.take("busy")
	clock.Advance(time.Second)

	for range rateLimitShards * 4 {
		l.take("trigger") // Gives every shard a chance to sweep
	}
	for i := range rateLimitShards * 4 {
		l.take(fmt.Sprintf("trigger-%d", i))
	}

	assert.Less(t, bucketCount(l), 1000, "refilled clients must be removed")

	r := l.take("busy")
	assert.True(t, r.allowed)
	assert.Less(t, r.remaining, 9, "a client still recovering must keep its state")
}

// Removing a client that's still recovering would hand it a full limit again.
func TestRateLimiter_CleanupKeepsClientsStillRecovering(t *testing.T) {
	clock := newFakeClock()
	l := newRateLimiter(RateLimitConfig{Limit: 10, Per: 10 * time.Second}, clock.Now)
	l.take("a") // Its shard is swept now, so the next sweep is due in 10s

	clock.Advance(9 * time.Second)
	for range 10 {
		l.take("a") // Empties it
	}

	clock.Advance(1500 * time.Millisecond) // The sweep is due, and "a" has refilled only 1.5
	r := l.take("a")

	assert.True(t, r.allowed)
	assert.Equal(t, 0, r.remaining, "it must keep its state, not start again with 10")
}

// Run with -race. A frozen clock means exactly the burst may get through.
func TestRateLimiter_ConcurrentRequestsNeverExceedTheLimit(t *testing.T) {
	l := newRateLimiter(RateLimitConfig{Limit: 100, Per: time.Hour}, newFakeClock().Now)

	var allowed atomic.Int64
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				if l.take("same-client").allowed {
					allowed.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(100), allowed.Load())
}

func rateLimitedRequest(remoteAddr string, headers ...string) *server.Context {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	return &server.Context{Request: req, Writer: httptest.NewRecorder()}
}

func okHandler(c *server.Context) *server.Response {
	return &server.Response{Success: true, Message: "ok", Code: http.StatusOK}
}

func TestRateLimit_Middleware(t *testing.T) {
	handler := RateLimit(2, time.Minute)(okHandler)

	first := rateLimitedRequest("203.0.113.7:5000")
	assert.Equal(t, 200, handler(first).Code)
	assert.Equal(t, "2", first.Writer.Header().Get("RateLimit-Limit"))
	assert.Equal(t, "1", first.Writer.Header().Get("RateLimit-Remaining"))
	assert.Equal(t, "30", first.Writer.Header().Get("RateLimit-Reset"))

	assert.Equal(t, 200, handler(rateLimitedRequest("203.0.113.7:5001")).Code, "the port isn't part of the client")

	refused := rateLimitedRequest("203.0.113.7:5002")
	resp := handler(refused)
	assert.Equal(t, 429, resp.Code)
	assert.False(t, resp.Success)
	assert.Equal(t, "Too Many Requests", resp.Message)
	assert.Equal(t, "30", refused.Writer.Header().Get("Retry-After"))
	assert.Equal(t, "0", refused.Writer.Header().Get("RateLimit-Remaining"))

	assert.Equal(t, 200, handler(rateLimitedRequest("198.51.100.1:5000")).Code, "another client")
	assert.Equal(t, 200, handler(rateLimitedRequest("[2001:db8::1]:443")).Code, "IPv6")
}

// The bypass this guards against: a client inventing a new address on every request.
func TestRateLimit_IgnoresForwardedForByDefault(t *testing.T) {
	handler := RateLimit(1, time.Minute)(okHandler)

	assert.Equal(t, 200, handler(rateLimitedRequest("203.0.113.7:1", "X-Forwarded-For", "1.1.1.1")).Code)
	assert.Equal(t, 429, handler(rateLimitedRequest("203.0.113.7:1", "X-Forwarded-For", "2.2.2.2")).Code)
}

func TestRateLimit_KeyFunc(t *testing.T) {
	handler := RateLimitWithConfig(RateLimitConfig{
		Limit:   1,
		Per:     time.Minute,
		KeyFunc: func(c *server.Context) string { return c.Request.Header.Get("X-API-Key") },
	})(okHandler)

	assert.Equal(t, 200, handler(rateLimitedRequest("1.1.1.1:1", "X-API-Key", "alpha")).Code)
	assert.Equal(t, 429, handler(rateLimitedRequest("2.2.2.2:1", "X-API-Key", "alpha")).Code,
		"the same key from another address is the same client")
	assert.Equal(t, 200, handler(rateLimitedRequest("1.1.1.1:1", "X-API-Key", "beta")).Code)

	for range 5 {
		unlimited := rateLimitedRequest("1.1.1.1:1")
		assert.Equal(t, 200, handler(unlimited).Code, "an empty key skips limiting")
		assert.Empty(t, unlimited.Writer.Header().Get("RateLimit-Limit"))
	}
}

func TestConcurrencyLimit(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 10)
	handler := ConcurrencyLimit(2)(func(c *server.Context) *server.Response {
		started <- struct{}{}
		<-release
		return okHandler(c)
	})

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			handler(rateLimitedRequest("1.1.1.1:1"))
		}()
	}
	<-started
	<-started

	busy := rateLimitedRequest("1.1.1.1:1")
	resp := handler(busy)
	assert.Equal(t, 503, resp.Code)
	assert.False(t, resp.Success)
	assert.Equal(t, "1", busy.Writer.Header().Get("Retry-After"))

	close(release)
	wg.Wait()
	assert.Equal(t, 200, handler(rateLimitedRequest("1.1.1.1:1")).Code, "places are given back")
}

func TestConcurrencyLimit_PanicGivesThePlaceBack(t *testing.T) {
	handler := ConcurrencyLimit(1)(func(c *server.Context) *server.Response { panic("boom") })

	for range 3 {
		assert.Panics(t, func() { handler(rateLimitedRequest("1.1.1.1:1")) })
	}

	// A 500 means it got in and panicked again; a 503 would mean the place was never returned.
	assert.Equal(t, 500, Recovery()(handler)(rateLimitedRequest("1.1.1.1:1")).Code)
}
