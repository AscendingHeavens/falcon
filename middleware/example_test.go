package middleware_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	falcon "github.com/ascendingheavens/falcon"
	"github.com/ascendingheavens/falcon/middleware"
)

func ExampleRateLimit() {
	app := falcon.New()
	app.Use(middleware.RateLimit(2, time.Minute)) // 2 requests a minute per client
	app.GET("/", func(c *falcon.Context) *falcon.Response { return c.OK("hello", nil) })

	for range 3 {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.7:4000" // The same client each time
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		fmt.Println(rec.Code, "remaining:", rec.Header().Get("RateLimit-Remaining"))
	}
	// Output:
	// 200 remaining: 1
	// 200 remaining: 0
	// 429 remaining: 0
}

func ExampleRateLimitWithConfig() {
	app := falcon.New()
	app.Use(middleware.RateLimitWithConfig(middleware.RateLimitConfig{
		Limit: 1,
		Per:   time.Minute,
		// Count requests per API key instead of per IP address
		KeyFunc: func(c *falcon.Context) string { return c.Request.Header.Get("X-API-Key") },
	}))
	app.GET("/", func(c *falcon.Context) *falcon.Response { return c.OK("hello", nil) })

	for _, key := range []string{"alpha", "alpha", "beta"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-API-Key", key)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		fmt.Println(key, rec.Code)
	}
	// Output:
	// alpha 200
	// alpha 429
	// beta 200
}

func ExampleConcurrencyLimit() {
	app := falcon.New()

	// At most 10 reports are generated at once; any more get a 503 straight away.
	app.UseIf(middleware.ConcurrencyLimit(10), "/reports/*")
	app.GET("/reports/:id", func(c *falcon.Context) *falcon.Response {
		return c.OK("Report ready", nil)
	})
}

func ExampleCORSWithConfig() {
	app := falcon.New()
	app.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     []string{"https://app.example.com"},
		AllowCredentials: true, // Allow cookies from that origin
	}))
	app.POST("/orders", func(c *falcon.Context) *falcon.Response { return c.Created("Order placed", nil) })

	// The preflight request a browser sends before a cross-origin POST
	req := httptest.NewRequest("OPTIONS", "/orders", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	fmt.Println(rec.Code)
	fmt.Println(rec.Header().Get("Access-Control-Allow-Origin"))
	fmt.Println(rec.Header().Get("Access-Control-Allow-Credentials"))
	// Output:
	// 204
	// https://app.example.com
	// true
}

func ExampleCSRF() {
	app := falcon.New()
	app.Use(middleware.CSRF())
	app.GET("/csrf-token", func(c *falcon.Context) *falcon.Response {
		return c.OK("OK", map[string]any{"token": c.Get("csrf_token")})
	})
	app.POST("/transfer", func(c *falcon.Context) *falcon.Response { return c.OK("Transferred", nil) })

	// The frontend first fetches a token, and the browser stores the cookie that comes with it.
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/csrf-token", nil))
	cookie := rec.Result().Cookies()[0]
	var body struct {
		Details struct{ Token string } `json:"details"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)

	post := func(token string) int {
		req := httptest.NewRequest("POST", "/transfer", strings.NewReader(""))
		req.AddCookie(cookie)
		if token != "" {
			req.Header.Set("X-CSRF-Token", token)
		}
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		return rec.Code
	}

	fmt.Println("with the token:", post(body.Details.Token))
	fmt.Println("forged, without it:", post(""))
	// Output:
	// with the token: 200
	// forged, without it: 403
}

// Middleware is a function wrapping a handler. Code after next(c) can change the response.
func ExampleMiddleware() {
	addHeader := func(next falcon.HandlerFunc) falcon.HandlerFunc {
		return func(c *falcon.Context) *falcon.Response {
			resp := next(c)
			c.Writer.Header().Set("X-Served-By", "falcon")
			return resp
		}
	}

	app := falcon.New()
	app.Use(addHeader)
	app.GET("/", func(c *falcon.Context) *falcon.Response { return c.OK("hello", nil) })

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	fmt.Println(rec.Header().Get("X-Served-By"), rec.Code == http.StatusOK)
	// Output: falcon true
}
