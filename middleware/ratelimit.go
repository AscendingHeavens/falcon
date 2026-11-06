package middleware

import (
	"errors"
	"fmt"
	"hash/maphash"
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ascendingheavens/falcon/server"
)

// ErrRateLimited is the cause of the 429 that RateLimit responds with, so that an
// ErrorMapper can recognise it with errors.Is.
var ErrRateLimited = errors.New("rate limit exceeded")

// RateLimit returns a middleware allowing each client limit requests every per, by IP
// address. Up to limit requests can arrive at once, so a page making several calls in
// quick succession isn't refused. Extra requests get a 429 with a Retry-After header.
//
// Limits are kept in memory, so with several instances of your app behind a load
// balancer, each one counts separately. Behind a proxy, every request appears to come
// from the proxy's address; see RateLimitConfig.KeyFunc for that case.
//
// Example:
//
//	app.Use(middleware.RateLimit(100, time.Minute))
//	app.UseIf(middleware.RateLimit(5, time.Minute), "/auth/login")
func RateLimit(limit int, per time.Duration) Middleware {
	return RateLimitWithConfig(RateLimitConfig{Limit: limit, Per: per})
}

// RateLimitWithConfig returns a rate limiting middleware with a custom configuration.
// It panics if the configuration can't work, so the mistake shows up at startup.
//
// Behind a load balancer or proxy, set KeyFunc to read the client's address from the
// header your proxy sets. Never trust that header without a proxy in front: clients can
// set it to anything, and a new value on every request would never be limited.
//
// Every response carries RateLimit-Limit, RateLimit-Remaining and RateLimit-Reset
// headers, so clients can slow down before they're refused.
func RateLimitWithConfig(cfg RateLimitConfig) Middleware {
	limiter := newRateLimiter(cfg, time.Now)
	keyFunc := cfg.KeyFunc
	if keyFunc == nil {
		keyFunc = clientIP
	}
	limitHeader := strconv.Itoa(int(limiter.burst))

	return func(next server.HandlerFunc) server.HandlerFunc {
		return func(c *server.Context) *server.Response {
			key := keyFunc(c)
			if key == "" {
				return next(c)
			}

			result := limiter.take(key)
			header := c.Writer.Header()
			header.Set("RateLimit-Limit", limitHeader)
			header.Set("RateLimit-Remaining", strconv.Itoa(result.remaining))
			header.Set("RateLimit-Reset", strconv.Itoa(ceilSeconds(result.reset)))

			if !result.allowed {
				header.Set("Retry-After", strconv.Itoa(max(1, ceilSeconds(result.retryAfter))))
				return c.Error(&server.HTTPError{Code: http.StatusTooManyRequests, Err: ErrRateLimited})
			}
			return next(c)
		}
	}
}

// clientIP returns the IP address the request's connection comes from.
func clientIP(c *server.Context) string {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return c.Request.RemoteAddr
	}
	return host
}

// ceilSeconds rounds a duration up to whole seconds, as rate limit headers use.
func ceilSeconds(d time.Duration) int {
	return int(math.Ceil(d.Seconds()))
}

// rateLimitShards splits the clients across locks, so concurrent requests from
// different clients rarely wait for each other.
const rateLimitShards = 64

// rateLimiter is a token bucket per client. Each bucket holds up to burst tokens and
// refills at rate tokens a second; a request takes one token, or is refused if there are none.
type rateLimiter struct {
	rate       float64 // Tokens added per second
	burst      float64 // Most tokens a bucket can hold
	sweepEvery time.Duration
	seed       maphash.Seed
	now        func() time.Time
	shards     [rateLimitShards]rateLimitShard
}

type rateLimitShard struct {
	mu        sync.Mutex
	buckets   map[string]bucket
	lastSweep time.Time
}

type bucket struct {
	tokens float64
	last   time.Time // When tokens was last brought up to date
}

// rateLimitResult is what a request learns from its bucket.
type rateLimitResult struct {
	allowed    bool
	remaining  int           // Whole tokens left
	reset      time.Duration // Until the bucket is full again
	retryAfter time.Duration // Until the next token, when refused
}

func newRateLimiter(cfg RateLimitConfig, now func() time.Time) *rateLimiter {
	if cfg.Limit <= 0 || cfg.Per <= 0 {
		panic(fmt.Sprintf("falcon: RateLimit needs a positive limit and period, got %d per %v", cfg.Limit, cfg.Per))
	}
	if cfg.Burst < 0 {
		panic(fmt.Sprintf("falcon: RateLimit needs a burst of at least 0, got %d", cfg.Burst))
	}

	burst := cfg.Burst
	if burst == 0 {
		burst = cfg.Limit
	}
	rate := float64(cfg.Limit) / cfg.Per.Seconds()

	// A bucket that has had time to fill up is the same as no bucket at all, so it can be
	// removed. Checking for those once per fill time keeps memory bounded by recent clients.
	fillTime := time.Duration(float64(burst) / rate * float64(time.Second))

	return &rateLimiter{
		rate:       rate,
		burst:      float64(burst),
		sweepEvery: max(fillTime, time.Second),
		seed:       maphash.MakeSeed(),
		now:        now,
	}
}

// take spends a token from key's bucket, if there's one to spend.
func (l *rateLimiter) take(key string) rateLimitResult {
	now := l.now()
	shard := &l.shards[maphash.String(l.seed, key)%rateLimitShards]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if shard.buckets == nil {
		shard.buckets = make(map[string]bucket)
	}
	if now.Sub(shard.lastSweep) >= l.sweepEvery {
		shard.sweep(l, now)
	}

	tokens := l.burst
	if b, ok := shard.buckets[key]; ok {
		tokens = l.refilled(b, now)
	}

	var result rateLimitResult
	if tokens >= 1 {
		tokens--
		result.allowed = true
	} else {
		result.retryAfter = l.duration(1 - tokens)
	}
	shard.buckets[key] = bucket{tokens: tokens, last: now}

	result.remaining = int(tokens)
	result.reset = l.duration(l.burst - tokens)
	return result
}

// refilled returns how many tokens a bucket holds by now.
func (l *rateLimiter) refilled(b bucket, now time.Time) float64 {
	return min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
}

// duration returns how long it takes to refill this many tokens.
func (l *rateLimiter) duration(tokens float64) time.Duration {
	return time.Duration(tokens / l.rate * float64(time.Second))
}

// sweep removes the buckets that have filled up again.
func (s *rateLimitShard) sweep(l *rateLimiter, now time.Time) {
	s.lastSweep = now
	for key, b := range s.buckets {
		if l.refilled(b, now) >= l.burst {
			delete(s.buckets, key)
		}
	}
}

// ConcurrencyLimit returns a middleware that lets at most n requests be handled at once,
// and answers any more with a 503 straight away instead of queueing them. Use it on slow
// or expensive routes, such as reports or calls to other services.
//
// It counts the time the handler and the middleware inside it run. The response is
// written after that, so a long download from c.File or c.Stream doesn't hold a place.
// Rejections aren't logged, because logging every one would add to the load at the
// moment the server can least afford it.
//
// Example: app.UseIf(middleware.ConcurrencyLimit(10), "/reports/*")
func ConcurrencyLimit(n int) Middleware {
	if n <= 0 {
		panic(fmt.Sprintf("falcon: ConcurrencyLimit needs a positive limit, got %d", n))
	}
	slots := make(chan struct{}, n)

	return func(next server.HandlerFunc) server.HandlerFunc {
		return func(c *server.Context) *server.Response {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
				return next(c)
			default:
				c.Writer.Header().Set("Retry-After", "1")
				return c.ErrorJSON("Too many requests in progress, try again shortly", nil, http.StatusServiceUnavailable)
			}
		}
	}
}
