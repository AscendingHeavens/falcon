package server

import "github.com/go-playground/validator/v10"

func (c *Context) SetValidator(v *validator.Validate) {
	c.Validator = v
}

// Validate runs validation on the target struct using the stored validator
// If none exists, it creates a default one on-the-fly
func (c *Context) Validate(target any) error {
	v := c.Validator
	if v == nil {
		v = validator.New()
	}
	return v.Struct(target)
}
