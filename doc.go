// Package falcon is a fast Go web framework for structured code. Every handler returns a
// response, every API response has the same shape, and every error takes the same path,
// so your frontend never has to guess what it's getting.
//
//   - One response shape: API responses are always {success, message, details, code}.
//     Anything else, like HTML or a file, has to come from a named helper.
//   - Handlers must return: a handler that returns nil gets a logged 500, not a silent
//     empty 200.
//   - One error path: return c.Error(err) works everywhere, and details stay hidden from
//     clients unless you turn on debug mode.
//   - Mistakes show up at startup: conflicting routes and malformed patterns panic with a
//     message naming the problem.
//   - Fast: zero allocations when routing, and in our benchmarks faster than Gin and Echo
//     on realistic API requests, with the least memory per request.
//
// # Installation
//
// Falcon requires Go 1.27 or newer.
//
//	go get github.com/ascendingheavens/falcon
//
// # Getting started
//
//	package main
//
//	import (
//		"log"
//
//		falcon "github.com/ascendingheavens/falcon"
//		"github.com/ascendingheavens/falcon/middleware"
//	)
//
//	func main() {
//		app := falcon.New()
//
//		app.Use(middleware.Logger())
//		app.Use(middleware.Recovery())
//
//		app.GET("/users/:id", func(c *falcon.Context) *falcon.Response {
//			return c.OK("User found", map[string]string{"id": c.Param("id")})
//		})
//
//		if err := app.Start(":8080"); err != nil {
//			log.Fatal(err)
//		}
//	}
//
// A request to /users/42 returns:
//
//	{"success":true,"message":"User found","details":{"id":"42"},"code":200}
//
// # Responses
//
// Handlers return a *Response. It only describes what to send: Falcon writes it after the
// handler and all middleware have returned, so middleware can still change it on the way out.
//
//	return c.OK("User found", user)             // 200
//	return c.Created("User created", user)      // 201
//	return c.Error(err)                         // The status for that error, see Errors
//	return c.ErrorJSON("Not allowed", nil, 403) // A failure with the code you choose
//
// Anything that isn't the JSON envelope has a named helper:
//
//	return c.String(200, "plain text")
//	return c.HTML(200, "<h1>Hello</h1>")
//	return c.Render(200, "users/show.html", user) // A template, see Templates
//	return c.File("./reports/latest.pdf")         // With range requests and caching
//	return c.Stream(200, "text/csv", reader)      // Without holding it in memory
//	return c.Redirect(302, "/login")
//	return c.NoContent() // 204
//
// # Errors
//
// c.Error turns any error into a response. Unknown errors become a generic 500: the real
// error is logged, and the client only sees it in debug mode.
//
//	user, err := repo.Find(id)
//	if err != nil {
//		return c.Error(err)
//	}
//	return c.OK("User found", user)
//
// Map your own errors to responses in one place with [WithErrorMapper], and use
// [NewHTTPError] for errors that already know their status:
//
//	return c.Error(falcon.NewHTTPError(403, "You can't edit this post"))
//
// # Binding and validation
//
//	type CreateUserRequest struct {
//		Name  string `json:"name" validate:"required,min=2"`
//		Email string `json:"email" validate:"required,email"`
//	}
//
//	func CreateUser(c *falcon.Context) *falcon.Response {
//		var req CreateUserRequest
//		if err := c.BindJSON(&req); err != nil {
//			return c.Error(err) // 400 for a malformed body, 413 if it's too large
//		}
//		if err := c.Validate(&req); err != nil {
//			return c.Error(err) // 422 listing the invalid fields, by their json names
//		}
//		return c.Created("User created", req)
//	}
//
// # Templates
//
//	renderer, err := falcon.NewTemplateRenderer("views", nil)
//	if err != nil {
//		log.Fatal(err)
//	}
//	app.SetRenderer(renderer)
//
// Templates are named by their path inside the folder, such as "users/show.html". Files in
// the layouts/ and partials/ folders are shared, and each page can define its own blocks.
// Output is escaped automatically, and a failing template gives a clean 500 instead of half
// a page. Use [NewTemplateRendererFS] with embed.FS to ship templates inside your binary.
//
// # Routing
//
//	app.GET("/users", listUsers)
//	app.GET("/users/me", currentUser)       // Static segments beat params
//	app.GET("/users/:id", getUser)          // c.Param("id")
//	app.GET("/static/*filepath", serveFile) // c.Param("filepath") is the rest of the path
//
//	v1 := app.Group("/api/v1")
//	v1.POST("/users", createUser)
//
// Falcon answers unknown paths with a 404 and the wrong method with a 405 and an Allow
// header, redirects a trailing slash to the route that exists, and handles HEAD and OPTIONS.
//
// # Middleware
//
//	app.Use(middleware.Logger())                         // Every request
//	app.UseIf(AuthMiddleware(), "/api/v1/*", "/admin/*") // Only these routes
//	app.UseIfNot(AuthMiddleware(), "/health", "/auth/*") // Every route except these
//
// The order is fixed, outermost first: global, then UseIf and UseIfNot, then group, then the
// handler. Patterns match whole segments, so "/api/*" matches "/api/users" but not
// "/apiary". The built-in middleware is in the middleware package.
//
// # Running the server
//
// [Server.Start], [Server.StartTLS] and [Server.StartAutoTLS] block until the server stops
// and return an error rather than exiting your program. For a graceful shutdown, call
// [Server.Shutdown] and wait for it to return before exiting.
package falcon
