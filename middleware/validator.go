package middleware

import (
	"github.com/ascendingheavens/falcon/server"
	"github.com/go-playground/validator/v10"
)

type ValidationConfig struct {
	Validator *validator.Validate
}

// ValidationMiddleware injects a validator instance into Context for reuse
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
