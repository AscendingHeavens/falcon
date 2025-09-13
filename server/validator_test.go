package server_test

import (
	"testing"

	"github.com/ascendingheavens/falcon/server"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
)

type TestStruct struct {
	Name  string `validate:"required"`
	Email string `validate:"required,email"`
}

func TestSetValidatorAndValidate(t *testing.T) {
	c := &server.Context{}

	// Case 1: Validate without setting validator (should create default)
	obj1 := TestStruct{Name: "Alice", Email: "alice@example.com"}
	err := c.Validate(&obj1)
	assert.NoError(t, err, "Validation should pass with valid struct")

	// Missing required field
	obj2 := TestStruct{Name: "", Email: "not-an-email"}
	err2 := c.Validate(&obj2)
	assert.Error(t, err2, "Validation should fail for missing Name and invalid Email")

	// Case 2: Set custom validator
	customValidator := validator.New()
	c.SetValidator(customValidator)

	obj3 := TestStruct{Name: "Bob", Email: "bob@example.com"}
	err3 := c.Validate(&obj3)
	assert.NoError(t, err3, "Validation should pass with valid struct using custom validator")

	// Ensure the validator stored is the same one
	assert.Equal(t, customValidator, c.Validator)
}
