package middleware

import (
	"github.com/ascendingheavens/falcon/server"
	"github.com/go-playground/validator/v10"
)

// ValidationConfig holds configuration for the validation middleware.
// Allows injecting a custom validator instance (from go-playground/validator/v10).
type ValidationConfig struct {
	Validator *validator.Validate // Optional custom validator. If nil, a new validator is created.
}

// ValidationMiddleware returns a middleware that injects a validator instance into the Context.
// The validator can then be accessed in handlers via c.Validator() (or c.SetValidator internally).
// Example usage:
//
//	app.Use(ValidationMiddleware(ValidationConfig{}))
func ValidationMiddleware(cfg ValidationConfig) Middleware {
	return func(next server.HandlerFunc) server.HandlerFunc {
		return func(c *server.Context) *server.Response {
			if cfg.Validator == nil {
				cfg.Validator = validator.New()
			}
			c.SetValidator(cfg.Validator)
			return next(c)
		}
	}
}
