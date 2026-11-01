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

type address struct {
	City string `json:"city" validate:"required"`
}

type signup struct {
	Email    string    `json:"email" validate:"required,email"`
	Name     string    `json:"name" validate:"min=2,max=5"`
	Age      int       `json:"age,omitempty" validate:"min=18"`
	Plan     string    `json:"plan" validate:"oneof=free pro"`
	Tags     []string  `json:"tags" validate:"min=1"`
	Address  address   `json:"address"`
	Contacts []address `json:"contacts" validate:"dive"`
	Code     string    `validate:"len=4"`
	Site     string    `json:"site" validate:"url"`
	Other    string    `json:"other" validate:"uuid"`
	Hidden   string    `json:"-" validate:"required"`
}

func TestValidate_ReportsFieldsByTheirJSONNames(t *testing.T) {
	c := &server.Context{}

	err := c.Validate(&signup{
		Email:    "nope",
		Name:     "x",
		Age:      3,
		Plan:     "gold",
		Contacts: []address{{City: "Pune"}, {}},
		Code:     "1",
		Site:     "nope",
		Other:    "nope",
	})

	var validationErr *server.ValidationError
	assert.ErrorAs(t, err, &validationErr)
	assert.Equal(t, []server.FieldError{
		{Field: "email", Rule: "email", Message: "must be a valid email"},
		{Field: "name", Rule: "min", Message: "must be at least 2 characters"},
		{Field: "age", Rule: "min", Message: "must be at least 18"},
		{Field: "plan", Rule: "oneof", Message: "must be one of: free pro"},
		{Field: "tags", Rule: "min", Message: "must be at least 1 items"},
		{Field: "address.city", Rule: "required", Message: "is required"},
		{Field: "contacts[1].city", Rule: "required", Message: "is required"},
		{Field: "Code", Rule: "len", Message: "must be exactly 4 characters"},
		{Field: "site", Rule: "url", Message: "must be a valid URL"},
		{Field: "other", Rule: "uuid", Message: "is invalid"},
		{Field: "Hidden", Rule: "required", Message: "is required"},
	}, validationErr.Fields)
	assert.Contains(t, err.Error(), "address.city")

	var original validator.ValidationErrors
	assert.ErrorAs(t, err, &original, "the validator's own error stays reachable")
}

func TestValidate_ErrorBecomesA422(t *testing.T) {
	c := &server.Context{}
	type login struct {
		Email string `json:"email" validate:"required"`
	}

	resp := c.Error(c.Validate(&login{}))

	assert.False(t, resp.Success)
	assert.Equal(t, 422, resp.Code)
	assert.Equal(t, "Validation failed", resp.Message)
	assert.Equal(t, []server.FieldError{{Field: "email", Rule: "required", Message: "is required"}}, resp.Details)
}

func TestValidate_NotAStructIsAnInternalError(t *testing.T) {
	c := &server.Context{}

	err := c.Validate("not a struct")

	assert.Error(t, err)
	assert.Equal(t, 500, c.Error(err).Code, "this is a bug in the handler, not bad input from the client")
}

func TestValidate_PassesWithNilError(t *testing.T) {
	c := &server.Context{}
	assert.True(t, c.Validate(&TestStruct{Name: "a", Email: "a@example.com"}) == nil)
}

func TestValidate_CustomValidatorWithoutJSONNames(t *testing.T) {
	c := &server.Context{}
	c.SetValidator(validator.New())
	type login struct {
		Email string `json:"email" validate:"required"`
	}

	var validationErr *server.ValidationError
	assert.ErrorAs(t, c.Validate(&login{}), &validationErr)
	assert.Equal(t, "Email", validationErr.Fields[0].Field)
}
