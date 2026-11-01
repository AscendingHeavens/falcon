package server

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
)

// FieldError describes one invalid field in a request.
type FieldError struct {
	Field   string `json:"field"`   // The name the client sends, e.g. "address.city"
	Rule    string `json:"rule"`    // The rule that failed, for code to act on, e.g. "required"
	Message string `json:"message"` // A readable description, e.g. "is required"
}

// ValidationError is returned by c.Validate when fields are invalid.
// c.Error turns it into a 422 response whose details list the fields.
type ValidationError struct {
	Fields []FieldError
	Err    error // The original error from the validator
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	names := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		names[i] = f.Field
	}
	return "validation failed: " + strings.Join(names, ", ")
}

// Unwrap returns the original error from the validator.
func (e *ValidationError) Unwrap() error {
	return e.Err
}

// defaultValidator is shared because a validator caches what it learns about
// each struct type, and creating one per request would throw that away.
var defaultValidator = sync.OnceValue(NewValidator)

// NewValidator creates a validator that reports fields by their json name. A custom
// validator passed to c.SetValidator needs the same setup to report the same names.
func NewValidator() *validator.Validate {
	v := validator.New()
	v.RegisterTagNameFunc(func(field reflect.StructField) string {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			return ""
		}
		return name
	})
	return v
}

// SetValidator sets the validator c.Validate uses for this request.
func (c *Context) SetValidator(v *validator.Validate) {
	c.Validator = v
}

// Validate runs validation on the target struct using the stored validator,
// or the default one if none is set. Invalid fields give a *ValidationError.
//
// Example:
//
//	if err := c.Validate(&req); err != nil {
//		return c.Error(err)
//	}
func (c *Context) Validate(target any) error {
	v := c.Validator
	if v == nil {
		v = defaultValidator()
	}

	err := v.Struct(target)
	if err == nil {
		return nil
	}

	var invalid validator.ValidationErrors
	if !errors.As(err, &invalid) {
		return err
	}

	fields := make([]FieldError, len(invalid))
	for i, fe := range invalid {
		fields[i] = FieldError{Field: fieldPath(fe), Rule: fe.Tag(), Message: fieldMessage(fe)}
	}
	return &ValidationError{Fields: fields, Err: err}
}

// fieldPath returns the field's path without the name of the root struct,
// which means nothing to a client: "Signup.address.city" becomes "address.city".
func fieldPath(fe validator.FieldError) string {
	_, path, _ := strings.Cut(fe.Namespace(), ".")
	return path
}

// fieldMessage returns a readable description of a failed rule.
func fieldMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email"
	case "url":
		return "must be a valid URL"
	case "oneof":
		return "must be one of: " + fe.Param()
	case "min", "gte":
		return "must be at least " + sized(fe)
	case "max", "lte":
		return "must be at most " + sized(fe)
	case "len":
		return "must be exactly " + sized(fe)
	default:
		return "is invalid"
	}
}

// sized returns a rule's limit in the unit that fits the field's type.
func sized(fe validator.FieldError) string {
	switch fe.Kind() {
	case reflect.String:
		return fmt.Sprintf("%s characters", fe.Param())
	case reflect.Slice, reflect.Array, reflect.Map:
		return fmt.Sprintf("%s items", fe.Param())
	default:
		return fe.Param()
	}
}
