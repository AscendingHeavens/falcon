package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRouter(t *testing.T) {
	// Setup
	router := NewRouter()
	testHandler := func(c *Context) *Response {
		return &Response{Success: true, Message: "ok", Code: 200}
	}

	router.Handle("GET", "/users", testHandler)
	router.Handle("GET", "/users/:id", testHandler)
	router.Handle("POST", "/users/:id/update", testHandler)

	tests := []struct {
		name       string
		method     string
		path       string
		wantFound  bool
		wantParams map[string]string
	}{
		{
			"static route match",
			"GET",
			"/users",
			true,
			map[string]string{},
		},
		{
			"parameter route match",
			"GET",
			"/users/123",
			true,
			map[string]string{"id": "123"},
		},
		{
			"parameter route with post method",
			"POST",
			"/users/456/update",
			true,
			map[string]string{"id": "456"},
		},
		{
			"wrong method",
			"POST",
			"/users",
			false,
			nil,
		},
		{
			"no route found",
			"GET",
			"/unknown",
			false,
			nil,
		},
		{
			"path length mismatch",
			"GET",
			"/users/123/extra",
			false,
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, params := router.FindHandler(tt.method, tt.path)
			if tt.wantFound {
				assert.NotNil(t, h)
				assert.Equal(t, tt.wantParams, params)
			} else {
				assert.Nil(t, h)
				assert.Nil(t, params)
			}
		})
	}
}

func TestContextSetGet(t *testing.T) {
	c := &Context{}

	t.Run("Set and Get a value", func(t *testing.T) {
		c.Set("foo", "bar")
		val := c.Get("foo")
		assert.Equal(t, "bar", val)
	})

	t.Run("Get returns nil for missing key", func(t *testing.T) {
		val := c.Get("nonexistent")
		assert.Nil(t, val)
	})

	t.Run("Overwrite existing key", func(t *testing.T) {
		c.Set("foo", 42)
		val := c.Get("foo")
		assert.Equal(t, 42, val)
	})

	t.Run("Values map is lazily initialized", func(t *testing.T) {
		c2 := &Context{}
		c2.Set("key", "value")
		assert.NotNil(t, c2.Values)
		assert.Equal(t, "value", c2.Values["key"])
	})
}
