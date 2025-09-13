package middleware_test

import (
	"testing"

	"github.com/ascendingheavens/falcon/middleware"
	"github.com/ascendingheavens/falcon/server"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
)

// Dummy handler for testing
func dummyHandlerCalled(c *server.Context) *server.Response {
	return &server.Response{Success: true, Message: "OK", Code: 200}
}

func TestValidationMiddleware_Injection(t *testing.T) {
	// Case 1: Middleware without a custom validator
	mw := middleware.ValidationMiddleware(middleware.ValidationConfig{})
	c := &server.Context{}

	resp := mw(dummyHandlerCalled)(c)

	assert.True(t, resp.Success)
	assert.Equal(t, "OK", resp.Message)
	assert.NotNil(t, c.Validator) // Validator injected
	assert.IsType(t, &validator.Validate{}, c.Validator)

	// Case 2: Middleware with a custom validator
	customValidator := validator.New()
	mwCustom := middleware.ValidationMiddleware(middleware.ValidationConfig{
		Validator: customValidator,
	})
	c2 := &server.Context{}
	resp2 := mwCustom(dummyHandlerCalled)(c2)

	assert.True(t, resp2.Success)
	assert.Equal(t, "OK", resp2.Message)
	assert.Equal(t, customValidator, c2.Validator)
}
