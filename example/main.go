package main

import (
	"net/http"

	falcon "github.com/ascendingheavens/falcon"
	"github.com/ascendingheavens/falcon/middleware"
)

func main() {
	app := falcon.New()

	// Global middlewares
	app.Use(middleware.Logger())
	app.Use(middleware.Recovery())
	app.Use(middleware.ProfilingMiddleware())

	// Conditional middleware
	app.UseIf("/api/v1/*", AuthMiddleware())

	// Top-level route
	app.GET("/ping", func(c *falcon.Context) *falcon.Response {
		return &falcon.Response{Success: true, Message: "pong", Code: 200}
	})
	app.GET("/search", func(c *falcon.Context) *falcon.Response {
		q := c.Query("q")
		return &falcon.Response{
			Success: true,
			Message: "Query received",
			Details: map[string]string{"query": q},
			Code:    200,
		}
	})

	app.GET("/html", func(c *falcon.Context) *falcon.Response {
		return c.HTML(200, "<h1>Serving HTML via Falcon</h1>") // send file content as HTML
	})

	app.GET("/docs", func(c *falcon.Context) *falcon.Response {
		return c.Redirect(302, "https://google.com")
	})

	// Route group
	v1 := app.Group("/api/v1")
	v1.GET("/users/:id", func(c *falcon.Context) *falcon.Response {
		id := c.Param("id")
		return &falcon.Response{Success: true, Message: "User found", Details: map[string]string{"id": id}, Code: 200}
	})

	auth := app.Group("/auth")
	auth.POST("/signup", Signup)
	auth.GET("/users/:id", func(c *falcon.Context) *falcon.Response {
		id := c.Param("id")
		return &falcon.Response{
			Success: true,
			Message: "User found",
			Details: map[string]string{"id": id},
			Code:    200,
		}
	})

	// Start server
	app.Start(":8080")
}

func AuthMiddleware() falcon.Middleware {
	return func(next falcon.HandlerFunc) falcon.HandlerFunc {
		return func(c *falcon.Context) *falcon.Response {
			if c.Request.Header.Get("Authorization") == "" {
				return &falcon.Response{
					Success: false,
					Message: "Unauthorized",
					Code:    401,
				}
			}
			return next(c)
		}
	}
}

func Signup(ctx *falcon.Context) *falcon.Response {
	var req struct{}
	if err := ctx.BindJSON(&req); err != nil {
		return nil
	}
	return &falcon.Response{
		Message: "Signup successful",
		Success: true,
		Code:    http.StatusOK,
	}
}
