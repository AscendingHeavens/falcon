package server_test

import (
	"fmt"
	"net/http/httptest"
	"strings"

	falcon "github.com/ascendingheavens/falcon"
)

func ExampleContext_Validate() {
	type Signup struct {
		Email string `json:"email" validate:"required,email"`
		Age   int    `json:"age" validate:"min=18"`
	}

	app := falcon.New()
	app.POST("/signup", func(c *falcon.Context) *falcon.Response {
		var req Signup
		if err := c.BindJSON(&req); err != nil {
			return c.Error(err)
		}
		if err := c.Validate(&req); err != nil {
			return c.Error(err) // 422, listing each invalid field by its json name
		}
		return c.Created("Signed up", nil)
	})

	req := httptest.NewRequest("POST", "/signup", strings.NewReader(`{"email":"nope","age":12}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	fmt.Println(rec.Code, rec.Body.String())
	// Output: 422 {"success":false,"message":"Validation failed","details":[{"field":"email","rule":"email","message":"must be a valid email"},{"field":"age","rule":"min","message":"must be at least 18"}],"code":422}
}

func ExampleContext_Query() {
	app := falcon.New()
	app.GET("/search", func(c *falcon.Context) *falcon.Response {
		return c.OK("Searching", map[string]any{
			"q":    c.Query("q"),
			"tags": c.QueryArray("tag"),
		})
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/search?q=falcon&tag=go&tag=web", nil))
	fmt.Println(rec.Body.String())
	// Output: {"success":true,"message":"Searching","details":{"q":"falcon","tags":["go","web"]},"code":200}
}

func ExampleContext_Stream() {
	app := falcon.New()
	app.GET("/export.csv", func(c *falcon.Context) *falcon.Response {
		// Any io.Reader works, such as a file or a database export, and isn't held in memory.
		return c.Stream(200, "text/csv", strings.NewReader("id,name\n1,Ada\n"))
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/export.csv", nil))
	fmt.Println(rec.Header().Get("Content-Type"))
	fmt.Print(rec.Body.String())
	// Output:
	// text/csv
	// id,name
	// 1,Ada
}

func ExampleContext_Copy() {
	app := falcon.New()
	app.POST("/orders/:id", func(c *falcon.Context) *falcon.Response {
		// The Context is reused once the handler returns, so a goroutine gets a copy.
		cp := c.Copy()
		go func() {
			_ = cp.Param("id") // Safe to use, for example to send a confirmation email
		}()
		return c.Created("Order placed", nil)
	})
}
