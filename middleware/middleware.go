package middleware

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/ascendingheavens/falcon/server"
)

var (
	ErrCSRFInvalid = errors.New("invalid CSRF token")
)

// Logger returns a middleware that logs each request's method, path, status and duration
// through the standard log package, which adds the date and time:
//
//	2026/11/07 13:54:30 GET "/users/42" 200 (182.4µs)
func Logger() Middleware {
	return func(next server.HandlerFunc) server.HandlerFunc {
		return func(c *server.Context) *server.Response {
			start := time.Now()
			resp := next(c)
			duration := time.Since(start)
			// The path is quoted because it comes from the client, and a newline in it could forge a log entry.
			log.Printf("%s %q %d (%v)", c.Request.Method, c.Request.URL.Path, statusOf(resp), duration)
			return resp
		}
	}
}

// statusOf returns the status a response will be sent with. A nil
// Response from a middleware further in is sent as a 500.
func statusOf(resp *server.Response) int {
	if resp == nil {
		return http.StatusInternalServerError
	}
	return resp.Code
}

// Panic Recovery
// Recovery returns a middleware that recovers from panics and responds with a 500.
// The panic and its stack trace are always logged, but the client only sees the panic
// message in debug mode. http.ErrAbortHandler passes through, because it's how a handler
// deliberately aborts a connection.
func Recovery() Middleware {
	return func(next server.HandlerFunc) server.HandlerFunc {
		return func(c *server.Context) (resp *server.Response) {
			defer func() {
				r := recover()
				if r == nil {
					return
				}
				if r == http.ErrAbortHandler {
					panic(r)
				}

				log.Printf("Recovered panic: %v\n%s", r, string(debug.Stack()))
				resp = c.Error(fmt.Errorf("panic: %v", r))
			}()

			return next(c)
		}
	}
}

// ProfilingMiddleware logs detailed timing info including handler execution and memory usage.
// Reading memory stats briefly stops the whole program, so use it for development only.
func ProfilingMiddleware() Middleware {
	return func(next server.HandlerFunc) server.HandlerFunc {
		return func(c *server.Context) *server.Response {
			start := time.Now()

			// Run handler
			resp := next(c)

			// Calculate elapsed
			elapsed := time.Since(start)

			// Capture some runtime stats (GC, mem)
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)

			log.Printf(
				"[PROFILE] Route: %q | Status: %d | Time: %v | Alloc: %dKB | Sys: %dKB | NumGC: %d",
				c.Request.URL.Path,
				statusOf(resp),
				elapsed,
				memStats.Alloc/1024,
				memStats.Sys/1024,
				memStats.NumGC,
			)

			return resp
		}
	}
}
