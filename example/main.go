// Command example is a small app using Falcon's main features: the JSON envelope,
// validation, central error mapping, templates, CSRF-protected forms, rate limiting
// and graceful shutdown.
//
// Run it from the repository root with: go run ./example
package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	falcon "github.com/ascendingheavens/falcon"
	"github.com/ascendingheavens/falcon/middleware"
)

// The templates are built into the binary, so the example runs from any directory.
//
//go:embed views
var views embed.FS

// Errors from the application's own logic. They know nothing about HTTP:
// mapErrors below decides what each one means to a client, in one place.
var (
	errUserNotFound = errors.New("user not found")
	errEmailTaken   = errors.New("email already registered")
)

func mapErrors(err error) *falcon.Response {
	switch {
	case errors.Is(err, errUserNotFound):
		return &falcon.Response{Message: "User not found", Code: 404}
	case errors.Is(err, errEmailTaken):
		return &falcon.Response{Message: "Email already registered", Code: 409}
	}
	return nil // Not ours: Falcon responds with a generic 500 and logs the real error
}

func main() {
	store := NewUserStore()

	// Add falcon.WithDebug() during development to see error details and reload templates.
	app := falcon.New(falcon.WithErrorMapper(mapErrors))

	templates, err := fs.Sub(views, "views")
	if err != nil {
		log.Fatal(err)
	}
	renderer, err := falcon.NewTemplateRendererFS(templates, nil)
	if err != nil {
		log.Fatal(err)
	}
	app.SetRenderer(renderer)

	// Global middleware runs on every request, including 404s
	app.Use(middleware.Logger())
	app.Use(middleware.Recovery())
	app.Use(middleware.CORS())
	app.Use(middleware.RateLimit(100, time.Minute))

	// Middleware for some routes only
	app.UseIf(middleware.CSRF(), "/contact")
	app.UseIf(AuthMiddleware(), "/api/*")
	app.UseIf(middleware.ConcurrencyLimit(5), "/export")

	// Pages
	app.GET("/", func(c *falcon.Context) *falcon.Response {
		return c.Render(200, "home.html", nil)
	})
	app.GET("/contact", ShowContact)
	app.POST("/contact", SendContact)
	app.GET("/old-home", func(c *falcon.Context) *falcon.Response {
		return c.Redirect(301, "/")
	})

	// The JSON envelope
	app.GET("/ping", func(c *falcon.Context) *falcon.Response {
		return c.OK("pong", nil)
	})
	app.GET("/search", func(c *falcon.Context) *falcon.Response {
		return c.OK("Query received", map[string]string{"query": c.Query("q")})
	})
	app.GET("/export", func(c *falcon.Context) *falcon.Response {
		return c.Stream(200, "text/csv", strings.NewReader("id,name\n1,Ada\n"))
	})

	// A route group, protected by AuthMiddleware above
	v1 := app.Group("/api/v1")
	v1.GET("/users/:id", GetUser(store))
	v1.POST("/users", CreateUser(store))

	if err := run(app, ":8080"); err != nil {
		log.Fatal(err)
	}
}

// run serves until the program is interrupted, then lets requests in progress finish.
func run(app *falcon.Server, addr string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	shutdownErr := make(chan error, 1)
	go func() {
		<-ctx.Done()
		timeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownErr <- app.Shutdown(timeout)
	}()

	if err := app.Start(addr); err != nil {
		return err
	}
	// Start returns as soon as Shutdown begins, so wait for Shutdown to finish.
	return <-shutdownErr
}

func AuthMiddleware() falcon.Middleware {
	return func(next falcon.HandlerFunc) falcon.HandlerFunc {
		return func(c *falcon.Context) *falcon.Response {
			if c.Request.Header.Get("Authorization") == "" {
				return c.Error(falcon.NewHTTPError(401, "Unauthorized"))
			}
			return next(c)
		}
	}
}

// ContactForm is filled from the contact page's form. The json tags name the
// fields in validation errors.
type ContactForm struct {
	Name    string `form:"name" json:"name" validate:"required"`
	Message string `form:"message" json:"message" validate:"required,min=5"`
}

func ShowContact(c *falcon.Context) *falcon.Response {
	return c.Render(200, "contact.html", map[string]any{
		"CSRFToken": c.Get("csrf_token"),
		"Form":      ContactForm{},
	})
}

func SendContact(c *falcon.Context) *falcon.Response {
	var form ContactForm
	if err := c.Bind(&form); err != nil {
		return c.Error(err)
	}

	// Show the form again with the problems, instead of the JSON a frontend would get.
	var invalid *falcon.ValidationError
	if err := c.Validate(&form); errors.As(err, &invalid) {
		return c.Render(422, "contact.html", map[string]any{
			"CSRFToken": c.Get("csrf_token"),
			"Form":      form,
			"Errors":    invalid.Fields,
		})
	} else if err != nil {
		return c.Error(err)
	}

	return c.Render(200, "thanks.html", form)
}

func GetUser(store *UserStore) falcon.HandlerFunc {
	return func(c *falcon.Context) *falcon.Response {
		user, err := store.Find(c.Param("id"))
		if err != nil {
			return c.Error(err) // 404, from mapErrors
		}
		return c.OK("User found", user)
	}
}

type CreateUserRequest struct {
	Name  string `json:"name" validate:"required,min=2"`
	Email string `json:"email" validate:"required,email"`
}

func CreateUser(store *UserStore) falcon.HandlerFunc {
	return func(c *falcon.Context) *falcon.Response {
		var req CreateUserRequest
		if err := c.BindJSON(&req); err != nil {
			return c.Error(err) // 400 for a malformed body, 413 if it's too large
		}
		if err := c.Validate(&req); err != nil {
			return c.Error(err) // 422 listing the invalid fields
		}

		user, err := store.Create(req.Name, req.Email)
		if err != nil {
			return c.Error(err) // 409, from mapErrors
		}
		return c.Created("User created", user)
	}
}

type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// UserStore stands in for a database.
type UserStore struct {
	mu    sync.Mutex
	users map[string]User
}

func NewUserStore() *UserStore {
	return &UserStore{users: make(map[string]User)}
}

func (s *UserStore) Find(id string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.users[id]
	if !ok {
		return User{}, errUserNotFound
	}
	return user, nil
}

func (s *UserStore) Create(name, email string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, user := range s.users {
		if user.Email == email {
			return User{}, errEmailTaken
		}
	}

	user := User{ID: strconv.Itoa(len(s.users) + 1), Name: name, Email: email}
	s.users[user.ID] = user
	return user, nil
}
