package falcon_test

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"testing/fstest"

	falcon "github.com/ascendingheavens/falcon"
)

func ExampleNew() {
	app := falcon.New()
	app.GET("/users/:id", func(c *falcon.Context) *falcon.Response {
		return c.OK("User found", map[string]string{"id": c.Param("id")})
	})

	// Send a request to the app, as a client would.
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/users/42", nil))
	fmt.Println(rec.Code, rec.Body.String())
	// Output: 200 {"success":true,"message":"User found","details":{"id":"42"},"code":200}
}

func ExampleServer_Group() {
	app := falcon.New()

	v1 := app.Group("/api/v1")
	v1.GET("/status", func(c *falcon.Context) *falcon.Response {
		return c.OK("API v1 is up", nil)
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/status", nil))
	fmt.Println(rec.Body.String())
	// Output: {"success":true,"message":"API v1 is up","code":200}
}

func ExampleServer_UseIfNot() {
	requireToken := func(next falcon.HandlerFunc) falcon.HandlerFunc {
		return func(c *falcon.Context) *falcon.Response {
			if c.Request.Header.Get("Authorization") == "" {
				return c.Error(falcon.NewHTTPError(401, "Unauthorized"))
			}
			return next(c)
		}
	}

	app := falcon.New()
	app.UseIfNot(requireToken, "/health") // Every route except /health needs a token
	app.GET("/health", func(c *falcon.Context) *falcon.Response { return c.OK("healthy", nil) })
	app.GET("/account", func(c *falcon.Context) *falcon.Response { return c.OK("your account", nil) })

	for _, path := range []string{"/health", "/account"} {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		fmt.Println(path, rec.Code)
	}
	// Output:
	// /health 200
	// /account 401
}

func ExampleWithErrorMapper() {
	errNotFound := errors.New("user not found") // An error from your own code

	app := falcon.New(falcon.WithErrorMapper(func(err error) *falcon.Response {
		if errors.Is(err, errNotFound) {
			return &falcon.Response{Message: "No such user", Code: 404}
		}
		return nil // Anything else gets the default handling
	}))
	app.GET("/users/:id", func(c *falcon.Context) *falcon.Response {
		return c.Error(fmt.Errorf("loading user %s: %w", c.Param("id"), errNotFound))
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/users/7", nil))
	fmt.Println(rec.Code, rec.Body.String())
	// Output: 404 {"success":false,"message":"No such user","code":404}
}

func ExampleNewHTTPError() {
	app := falcon.New()
	app.DELETE("/posts/:id", func(c *falcon.Context) *falcon.Response {
		return c.Error(falcon.NewHTTPError(403, "You can't delete this post"))
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("DELETE", "/posts/1", nil))
	fmt.Println(rec.Code, rec.Body.String())
	// Output: 403 {"success":false,"message":"You can't delete this post","code":403}
}

func ExampleNewTemplateRendererFS() {
	// Templates usually come from embed.FS or a folder; this keeps the example self-contained.
	views := fstest.MapFS{
		"layouts/base.html": {Data: []byte(`<main>{{template "content" .}}</main>`)},
		"users/show.html":   {Data: []byte(`{{template "layouts/base.html" .}}{{define "content"}}<h1>{{.}}</h1>{{end}}`)},
	}
	renderer, err := falcon.NewTemplateRendererFS(views, nil)
	if err != nil {
		panic(err)
	}

	app := falcon.New()
	app.SetRenderer(renderer)
	app.GET("/users/:name", func(c *falcon.Context) *falcon.Response {
		return c.Render(200, "users/show.html", c.Param("name"))
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/users/Ada", nil))
	fmt.Println(rec.Body.String())
	// Output: <main><h1>Ada</h1></main>
}
