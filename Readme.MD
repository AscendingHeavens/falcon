# Falcon Framework

<p align="center">
  <img src="./falcon.png" alt="Falcon Logo" width="200">
</p>


A fast Go web framework for structured code. In Falcon, every handler returns a response, every API response has the same shape, and every error takes the same path, so your frontend never has to guess what it's getting. In our benchmarks it's also faster than Gin and Echo on realistic API requests, and uses the least memory per request of the three.

> [GO DOCS](https://pkg.go.dev/github.com/ascendingheavens/falcon)  
Note: This repository was transferred to AscendingHeavens/falcon. Due to Go module proxy caching, the official Go docs still point to the original path because the module was already published and tagged under the old repository.

---

## Why Falcon

* **One response shape.** API responses are always `{success, message, details, code}`. Anything else, like HTML or a file, has to come from a named helper.
* **Handlers must return.** There's no writing an error, forgetting the `return` and carrying on. A handler that returns `nil` gets a logged 500, not a silent empty 200.
* **One error path.** `return c.Error(err)` works everywhere. You map your errors to responses in one place, and details stay hidden from clients unless you turn on debug mode.
* **Mistakes show up at startup.** Conflicting routes and malformed patterns panic with a message naming the problem, before you serve a single request.
* **Predictable middleware.** It runs in a fixed order and applies no matter where you register it.
* **Safe defaults.** Request size limits, connection timeouts, CSRF and CORS that are secure out of the box, and rate limiting that can't be bypassed with a forged header.
* **Fast.** Zero allocations when routing and, in our benchmarks, about 2x faster JSON responses than Gin and Echo and the least memory per request. See [Performance](#performance).

---

## Features

| Area | What's included |
|---|---|
| Routing | Radix tree router with `:params` and `*catch-alls`, route groups, automatic 404, 405, HEAD, OPTIONS and trailing-slash redirects, conflicts reported at startup |
| Responses | The JSON envelope, plus helpers for text, HTML, files with range requests and caching, streams, redirects and templates |
| Errors | `c.Error`, typed `HTTPError`s, one central error mapper, and debug mode for development |
| Input | Binding JSON, XML, forms and multipart, validation that reports fields by their JSON names, query params, file uploads |
| Templates | HTML templates with layouts and partials, from a folder or embedded in the binary, reloading in debug mode |
| Middleware | Global, per group, or by path with `UseIf` and `UseIfNot`, in a fixed order |
| Built-in middleware | Logger, Recovery, Profiling, CORS, CSRF, JWT, validation, rate limiting, concurrency limiting |
| Server | Request body limits, connection timeouts, TLS and automatic Let's Encrypt certificates, graceful shutdown |

---

## Installation

Requires Go 1.27 or newer.

```bash
go get github.com/ascendingheavens/falcon
```

---

## Quick start

```go
package main

import (
	"log"

	falcon "github.com/ascendingheavens/falcon"
	"github.com/ascendingheavens/falcon/middleware"
)

func main() {
	app := falcon.New()

	app.Use(middleware.Logger())
	app.Use(middleware.Recovery())

	app.GET("/users/:id", func(c *falcon.Context) *falcon.Response {
		return c.OK("User found", map[string]string{"id": c.Param("id")})
	})

	if err := app.Start(":8080"); err != nil {
		log.Fatal(err)
	}
}
```

```bash
$ curl localhost:8080/users/42
{"success":true,"message":"User found","details":{"id":"42"},"code":200}
```

For a fuller app, with validation, error mapping, templates, a CSRF-protected form, rate limiting and graceful shutdown, see [example/main.go](example/main.go). Run it with `go run ./example`.

---

## Responses

Handlers return a `*falcon.Response`. It only describes what to send: Falcon writes it after the handler and all middleware have returned, so middleware can still change it on the way out.

For the JSON envelope, use a helper or build it yourself:

| Code | Sends |
|---|---|
| `c.OK(message, details)` | 200 |
| `c.Created(message, details)` | 201 |
| `c.Error(err)` | The status and message for that error, see [Errors](#errors) |
| `c.ErrorJSON(message, details, code)` | A failure with the code you choose |
| `c.JSON(success, message, details, code)` | Every field given explicitly |
| `&falcon.Response{Success: true, Message: "OK", Code: 200}` | The same, built by hand. `Code` is required |

Everything that isn't the envelope has a named helper:

| Code | Sends |
|---|---|
| `c.String(code, text)` | Plain text |
| `c.HTML(code, html)` | HTML |
| `c.Render(code, name, data)` | An HTML template, see [Templates](#templates) |
| `c.Blob(code, data, contentType)` | Raw bytes |
| `c.File(path)` | A file from disk, with range requests and caching. Never build the path directly from user input |
| `c.Stream(code, contentType, reader)` | A body copied as it's sent, without holding it in memory |
| `c.Redirect(code, location)` | A redirect |
| `c.NoContent()` | 204 with no body |

To pass values between middleware and handlers, use `c.Set("user", user)` and `c.Get("user")`.

### Taking over the writer

Websockets and server-sent events have to write as they go. Set `c.Handled = true` once you've taken over `c.Writer`, and Falcon won't write anything. This is the one way out of the rules above, so it's explicit and easy to search for. Long-lived connections also need to clear the server's write timeout themselves, using `http.NewResponseController`.

---

## Errors

`c.Error` turns any error into a response, so handlers stay short:

```go
user, err := repo.Find(id)
if err != nil {
	return c.Error(err)
}
return c.OK("User found", user)
```

It decides the response in this order:

1. Your error mapper, if you set one and it recognises the error.
2. A validation error becomes a 422 listing the fields.
3. An `HTTPError` becomes a response with its code and message.
4. A request body over the limit becomes a 413.
5. Anything else becomes a generic 500. The real error is logged, and the client only sees it in debug mode.

**Map your own errors in one place**, so your business logic doesn't need to know about HTTP:

```go
app := falcon.New(falcon.WithErrorMapper(func(err error) *falcon.Response {
	switch {
	case errors.Is(err, repo.ErrNotFound):
		return &falcon.Response{Message: "User not found", Code: 404}
	case errors.Is(err, repo.ErrEmailTaken):
		return &falcon.Response{Message: "Email already registered", Code: 409}
	}
	return nil // Not ours: use the default handling
}))
```

**Errors that already know their status** can use `HTTPError`. Its `Err` field holds the underlying cause, which is logged on 5xx errors and only shown to clients in debug mode:

```go
return c.Error(falcon.NewHTTPError(403, "You can't edit this post"))
return c.Error(&falcon.HTTPError{Code: 503, Message: "Try again soon", Err: err})
```

---

## Binding and validation

```go
type CreateUserRequest struct {
	Name  string `json:"name" validate:"required,min=2"`
	Email string `json:"email" validate:"required,email"`
}

func CreateUser(c *falcon.Context) *falcon.Response {
	var req CreateUserRequest
	if err := c.BindJSON(&req); err != nil {
		return c.Error(err) // 400 for a malformed body, 413 if it's too large
	}
	if err := c.Validate(&req); err != nil {
		return c.Error(err) // 422 listing the invalid fields
	}
	return c.Created("User created", req)
}
```

Validation errors give your frontend something it can act on, using the same field names it sends:

```json
{
  "success": false,
  "message": "Validation failed",
  "details": [
    {"field": "name", "rule": "min", "message": "must be at least 2 characters"},
    {"field": "email", "rule": "email", "message": "must be a valid email"}
  ],
  "code": 422
}
```

Rules come from [go-playground/validator](https://github.com/go-playground/validator). To use your own validator, set it with `c.SetValidator` or `middleware.ValidationMiddleware`, and create it with `server.NewValidator()` to keep the JSON field names.

| Function | Reads |
|---|---|
| `c.Bind(&dest)` | JSON, XML, form or multipart, based on `Content-Type` |
| `c.BindJSON(&dest)`, `c.BindXML(&dest)` | That format only |
| `c.ShouldBind(&dest)` and friends | The same, returning the underlying error instead of an `HTTPError` |
| `c.Param("id")` | A path parameter from `/users/:id` |
| `c.Query("q")`, `c.QueryArray("tag")` | Query parameters |
| `c.FormValue("name")`, `c.PostFormValue("name")` | A form field, from the body and query, or the body only |
| `c.FormFile("avatar")` | An uploaded file |

Struct fields bind from forms using a `form` tag, or the lowercase field name if there isn't one. Every way of reading the body respects the server's body limit, which is 10MB by default and also covers file uploads.

---

## Templates

```go
renderer, err := falcon.NewTemplateRenderer("views", nil)
if err != nil {
	log.Fatal(err)
}
app.SetRenderer(renderer)

app.GET("/users/:id", func(c *falcon.Context) *falcon.Response {
	return c.Render(200, "users/show.html", map[string]string{"Name": c.Param("id")})
})
```

Templates are named by their path inside the folder, such as `users/show.html`, so pages in different folders never collide. Files in the top-level `layouts/` and `partials/` folders are shared with every page, and each page is parsed separately, so every page can define its own `content` block:

```html
<!-- views/layouts/base.html -->
<html><body>{{template "partials/nav.html" .}}<main>{{template "content" .}}</main></body></html>

<!-- views/users/show.html -->
{{template "layouts/base.html" .}}
{{define "content"}}<h1>{{.Name}}</h1>{{end}}
```

* Output is escaped automatically, so user data can't inject scripts.
* A page is rendered in full before anything is sent, so a template that fails halfway gives a clean 500 instead of half a page.
* Use `falcon.NewTemplateRendererFS` with `embed.FS` to ship templates inside your binary.
* With `falcon.WithDebug()`, templates reload on every request, so edits show up without a restart. A template that no longer parses gives a 500 with the reason until it's fixed.
* Template files can end in `.html`, `.tmpl` or `.gohtml`. The second argument to the constructors is an optional `template.FuncMap`.

---

## Routing

```go
app.GET("/users", listUsers)
app.GET("/users/me", currentUser)       // Static segments beat params
app.GET("/users/:id", getUser)          // c.Param("id")
app.GET("/static/*filepath", serveFile) // c.Param("filepath") is the rest of the path

v1 := app.Group("/api/v1")
v1.Use(rateLimit)
v1.POST("/users", createUser)
```

A param matches one whole, non-empty segment. Matching prefers static over param over catch-all, and falls back when a branch turns out to be a dead end, so `/users/me/profile` still reaches `/users/:id/profile`.

Falcon also answers these for you:

| Request | Response |
|---|---|
| Unknown path | 404 envelope |
| Known path, wrong method | 405 envelope with an `Allow` header |
| `/users/` when only `/users` exists, or the reverse | Redirect: 301 for GET and HEAD, 308 for other methods |
| HEAD on a GET route | The GET handler runs, and the body is dropped |
| OPTIONS on a known path | 204 with an `Allow` header, unless you register your own |

Two routes that can't coexist panic when you register them, naming both:

```
falcon: route "GET /users/:name" conflicts with "GET /users/:id": a param can't be named both :name and :id at the same position
```

---

## Middleware

```go
app.Use(middleware.Logger())                         // Every request
app.UseIf(AuthMiddleware(), "/api/v1/*", "/admin/*") // Only these routes
app.UseIfNot(AuthMiddleware(), "/health", "/auth/*") // Every route except these
v1.Use(rateLimit)                                    // Routes in this group
```

Middleware is a function wrapping a handler. Code after `next(c)` can read and change the response:

```go
func Timing() falcon.Middleware {
	return func(next falcon.HandlerFunc) falcon.HandlerFunc {
		return func(c *falcon.Context) *falcon.Response {
			start := time.Now()
			resp := next(c)
			c.Writer.Header().Set("X-Duration", time.Since(start).String())
			return resp
		}
	}
}
```

**Order is fixed**, outermost first: global, then `UseIf` and `UseIfNot`, then group, then the handler. Within each level, the first one registered runs first. It doesn't matter whether you register middleware before or after your routes, but everything must be registered before the first request. Registering later panics.

**Patterns match whole segments:** `/api/*` matches `/api` and `/api/users/:id`, but not `/apiary`. A pattern with no wildcard matches only the route with exactly that path. Patterns are compared with your routes once, at startup, so they cost nothing per request.

**Requests with no route** only run global middleware, because there's no route for a pattern to match. That covers 404s, 405s and automatic OPTIONS responses.

### Built-in middleware

| Middleware | Purpose |
|---|---|
| `middleware.Logger()` | Logs method, path, status and duration |
| `middleware.Recovery()` | Turns panics into a 500. The stack is always logged, and the panic message only reaches clients in debug mode |
| `middleware.ProfilingMiddleware()` | Logs duration and memory stats. It briefly stops the whole program on each request, so use it for development only |
| `middleware.CORS()`, `middleware.CORSWithConfig(cfg)` | Cross-origin requests |
| `middleware.CSRF()`, `middleware.CSRFWithConfig(cfg)` | CSRF protection, for API calls and HTML forms |
| `middleware.JWTMiddleware(secret)` | Validates a bearer token, and stores its claims under `c.Get("user")` |
| `middleware.ValidationMiddleware(cfg)` | Sets the validator for `c.Validate` |
| `middleware.RateLimit(limit, per)`, `middleware.RateLimitWithConfig(cfg)` | Limits how often each client can call you |
| `middleware.ConcurrencyLimit(n)` | Limits how many requests are handled at once |

### CORS

Register CORS with `app.Use`. Preflight requests only run global middleware, so CORS set with `UseIf` or on a group won't answer them, and browsers will block the real request.

`CORS()` allows every origin without credentials. To allow cookies or `Authorization` across origins, list the origins:

```go
app.Use(middleware.CORSWithConfig(middleware.CORSConfig{
	AllowOrigins:     []string{"https://app.example.com"},
	AllowCredentials: true,
}))
```

Combining `*` with `AllowCredentials` panics at startup, because it would let any website make logged-in requests for your users.

### CSRF

A cookie holds a random value, and each unsafe request must send a token derived from it, either in the `X-CSRF-Token` header or in a `csrf_token` form field. The cookie is HttpOnly, so give the token to your frontend from a handler:

```go
app.Use(middleware.CSRF())

app.GET("/csrf-token", func(c *falcon.Context) *falcon.Response {
	return c.OK("OK", map[string]any{"token": c.Get("csrf_token")})
})
```

For HTML forms, pass it to the template and add it as a hidden field:

```html
<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
```

Requests with a missing or wrong token get a 403. The token is never read from the URL, where it would leak into logs. By default the secret is random, so tokens stop working on restart and aren't shared between servers. Set `Secret` with `CSRFWithConfig` if either matters.

### Rate limiting

```go
app.Use(middleware.RateLimit(100, time.Minute))                // 100 requests a minute per client
app.UseIf(middleware.RateLimit(5, time.Minute), "/auth/login") // Stricter on one route
app.UseIf(middleware.ConcurrencyLimit(10), "/reports/*")       // At most 10 at once
```

* Clients over the limit get a 429 with a `Retry-After` header. Every response carries `RateLimit-Limit`, `RateLimit-Remaining` and `RateLimit-Reset`, so clients can slow down before they're refused.
* Clients are told apart by their connection's IP address, never by `X-Forwarded-For`, which any client can forge to avoid the limit. Behind a load balancer, set `KeyFunc` in `RateLimitWithConfig` to read the address your proxy sets. You can also limit by API key or user ID the same way.
* Limits are kept in memory, so with several instances of your app, each one counts separately.
* `ConcurrencyLimit` answers with a 503 straight away when it's full, instead of letting requests pile up on slow routes.
* To change the 429 response, match `middleware.ErrRateLimited` in your error mapper.

---

## Server options

```go
app := falcon.New(
	falcon.WithDebug(),
	falcon.WithBodyLimit(50<<20),
)
```

| Option | Default | Purpose |
|---|---|---|
| `WithDebug()` | Off | Includes the underlying error in error responses and reloads templates. Never use it in production |
| `WithErrorMapper(fn)` | None | Maps your errors to responses, see [Errors](#errors) |
| `WithBodyLimit(bytes)` | 10MB | Maximum request body, **including file uploads**. Larger bodies get a 413 |
| `WithMultipartMemory(bytes)` | 32MB | How much of a multipart form stays in memory before going to temporary files |
| `WithTimeouts(timeouts)` | See below | Connection timeouts. Any limit left at zero is turned off |

Default timeouts are 10s to read headers, 60s to read a request, 60s to write a response and 120s for an idle connection. `c.File` and `c.Stream` are exempt from the write timeout, so large downloads aren't cut off.

---

## Running the server

`Start`, `StartTLS` and `StartAutoTLS` block until the server stops, and return an error rather than exiting your program.

For a graceful shutdown, call `Shutdown` when your program is told to stop. `Start` returns as soon as shutdown begins, so wait for `Shutdown` to finish before exiting, or requests in progress get cut off:

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
defer stop()

shutdownErr := make(chan error, 1)
go func() {
	<-ctx.Done()
	timeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shutdownErr <- app.Shutdown(timeout)
}()

if err := app.Start(":8080"); err != nil {
	log.Fatal(err)
}
if err := <-shutdownErr; err != nil {
	log.Fatal(err)
}
```

---

## Using goroutines in a handler

Falcon reuses each `Context` for later requests once the handler returns. A goroutine that outlives the handler must use a copy:

```go
cp := c.Copy()
go audit(cp.Param("id"), cp.Get("user"))
```

A copy can't respond to the client: anything written to it is discarded.

---

## Performance

### What the numbers mean

Each benchmark measures how much work the framework itself adds to a request. Two numbers matter:

* **Time per request.** This is how long the framework itself spends on a request. Dividing one second by it gives a theoretical ceiling for the framework's overhead alone: Falcon's 921 ns on a realistic request works out to about 1.1 million requests a second per core, against about 800,000 for Echo and 390,000 for Gin. That isn't a measurement of real throughput, which depends on your database, business logic and network, and those usually take most of a request's time. So it doesn't mean your app gets 2.7x faster than with Gin. Falcon introduces very little framework overhead, leaving more CPU capacity for application logic and concurrent requests, and lower framework overhead can potentially improve throughput and reduce CPU use under comparable workloads.
* **Memory allocations per request.** Every allocation is memory Go's garbage collector has to clean up later, which costs CPU time and can cause latency spikes under load. Falcon allocates 6 times per realistic request, against 10 for Echo and 22 for Gin.

**Zero allocations when routing** means finding the right handler creates no garbage at all, whether your app has 5 routes or 500.

### Results

Median of 5 runs. Lower is better.

| Benchmark | What it measures | **Falcon** | Gin | Echo |
|---|---|---|---|---|
| Realistic request | 222 routes, logging, panic recovery, a path param, two query params and a JSON response: a typical API call | **921 ns**, 6 allocs | 2,583 ns, 22 allocs | 1,245 ns, 10 allocs |
| Realistic request, no logging | The same without logging, because Gin's default log line contains more than Falcon's and Echo's, which are set up to log the same fields | **482 ns**, 3 allocs | 1,610 ns, 11 allocs | 715 ns, 5 allocs |
| JSON response | Encoding the same response body | **312 ns** | 715 ns | 639 ns |
| Static route among 222 | Finding a fixed path like `/user/repos` in a large app | **25.4 ns** | 29.0 ns | 35.2 ns |
| Param route among 222 | Finding a path with 3 params, like `/repos/:owner/:repo/issues/:number` | **37.4 ns** | 44.6 ns | 58.1 ns |
| All 222 routes | One request to every route of the app | **8,661 ns** | 10,026 ns | 13,829 ns |
| One static route | The smallest possible app, showing the framework's fixed cost | **17.5 ns** | 20.8 ns | 25.2 ns |
| One param route | The smallest app with a param | 23.3 ns | 23.4 ns | 29.2 ns |
| Long static path | An 85-character path, the hardest case for comparing paths | 23.3 ns | **21.3 ns** | 44.9 ns |
| Two query params | Reading `?q=falcon&page=2` | 103 ns | 755 ns | **73 ns** |

In these benchmarks, Falcon is faster on realistic requests, JSON and most routing, and uses the least memory per request. Gin is faster on very long paths and level on a single param route, and Echo is faster at reading query params on their own.

### Methodology and disclaimer

These benchmarks measure framework-level overhead under controlled conditions. They aren't intended to represent the performance of every application or production workload.

* **Environment:** an AMD Ryzen 7 9700X on Windows, with Go 1.27.3, Gin v1.12.0 and Echo v5.4.4. Results may vary with hardware, operating system, Go version, configuration, middleware and workload.
* **Equivalent work:** all three frameworks use the same route definitions and send the same response payloads, and tests check that they produce identical JSON. The handlers for the routing benchmarks do no work in any of the three, so that only routing is measured.
* **Routes:** the route set is modelled on the GitHub API route set from [go-http-routing-benchmark](https://github.com/julienschmidt/go-http-routing-benchmark), without its catch-all routes.
* **Allocation counts** depend only on the code, so they're the most reliable numbers to compare across machines.

Falcon, Gin and Echo are independent software projects. This comparison is an independently conducted technical evaluation, and doesn't imply any affiliation with or endorsement by the maintainers of Gin or Echo.

### Run them yourself

```bash
make bench
```

Each run is saved to `bench/results/benchmark_test_dd-mm-yyyy-hh-mm.txt`. If your results differ from ours, especially on other hardware, please share them in an issue.

---

## Development

The Makefile works the same on Windows, macOS and Linux. Every tool runs through `go run` at a pinned version, so you don't need to install any of them. You do need Go and `make`:

- **Go**: install from [go.dev/dl](https://go.dev/dl).
- **make**: already there on most Linux distributions and on macOS once the Xcode command line tools are installed (`xcode-select --install`). On Windows it isn't included, so install it with `choco install make`, `scoop install make` or `winget install GnuWin32.Make`, or use Git Bash or WSL. If you'd rather not install it, you can run the `go run` commands from the Makefile directly.

| Command | Does |
|---|---|
| `make check` | Everything below that CI should run: formatting, vet, lint, tests with coverage, and the vulnerability scan |
| `make test` | All tests |
| `make cover` | The tests, failing if coverage drops below 85% |
| `make race` | The tests with the race detector. This needs a C compiler such as gcc |
| `make lint`, `make fix-lint` | golangci-lint |
| `make vuln` | govulncheck, for known vulnerabilities in the code's dependencies |
| `make fmt`, `make fmt-check` | Format the code, or check that it's formatted |
| `make bench` | The benchmarks, saved to `bench/results` |

---

## Issues and ideas

Falcon gets better with every report, so please [open an issue](https://github.com/AscendingHeavens/falcon/issues) for:

* **Bugs:** what you did, what you expected and what happened instead. A small piece of code that shows the problem helps most.
* **Feature requests:** what you're trying to build, and what's missing or awkward in Falcon today.
* **Performance:** benchmark results from your own machine, or a case where Falcon is slower than you'd expect.
* **Documentation:** anything in this README that was unclear, wrong or missing.

Security problems are the exception: please [report them privately](https://github.com/AscendingHeavens/falcon/security/advisories/new) instead of in a public issue, so they can be fixed before they're widely known.

---

## Contributions

Contributions to Falcon are welcome and encouraged! Here's how you can help:

### Code Contributions

1. **Open an issue first** for anything bigger than a small fix, so we can agree on the approach before you spend time on it.
2. **Fork the repository**.
3. **Create a new branch** for your feature or bug fix: `git checkout -b feature/my-feature`.
4. **Write tests** for your changes.
5. **Make your changes** following the project's coding standards.
6. **Run `make check`** locally to ensure nothing breaks.
7. **Commit your changes** with clear and descriptive messages.
8. **Push your branch** to your fork.
9. **Open a pull request** against the `main` branch of the main repository.

### Documentation

* Improve documentation, examples, or guides.
* Correct typos or clarify confusing explanations.

### Code Reviews

* Review open pull requests and provide feedback.
* Test new features and verify fixes.

### Style & Standards

* Follow Go idiomatic practices.
* Keep commits focused and concise.
* Write descriptive commit messages.

We appreciate all contributions, big or small, and they help make Falcon better for everyone!



## License
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
