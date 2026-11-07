// Package middleware provides Falcon's built-in middleware.
//
//	app.Use(middleware.Logger())
//	app.Use(middleware.Recovery())
//	app.Use(middleware.CORS())
//	app.Use(middleware.RateLimit(100, time.Minute))
//
//	app.UseIf(middleware.RateLimit(5, time.Minute), "/auth/login")
//	app.UseIf(middleware.ConcurrencyLimit(10), "/reports/*")
//
// A middleware wraps a handler. Code after next(c) can read and change the response:
//
//	func Timing() middleware.Middleware {
//		return func(next server.HandlerFunc) server.HandlerFunc {
//			return func(c *server.Context) *server.Response {
//				start := time.Now()
//				resp := next(c)
//				c.Writer.Header().Set("X-Duration", time.Since(start).String())
//				return resp
//			}
//		}
//	}
//
// # CORS
//
// Register CORS with app.Use, so that it can answer preflight requests, which only run
// global middleware. [CORS] allows every origin without credentials. To allow cookies
// across origins, list the origins with [CORSWithConfig].
//
// # CSRF
//
// [CSRF] expects a token in the X-CSRF-Token header or a csrf_token form field. Handlers
// read it with c.Get("csrf_token"), to return it to a frontend or render it into a form.
//
// # Rate limiting
//
// [RateLimit] tells clients apart by their connection's IP address, never by a header a
// client can forge. Clients over the limit get a 429 with a Retry-After header, and every
// response carries RateLimit-Limit, RateLimit-Remaining and RateLimit-Reset headers. Limits
// are kept in memory, so with several instances of your app each one counts separately.
// [ConcurrencyLimit] caps how many requests are handled at once.
package middleware
