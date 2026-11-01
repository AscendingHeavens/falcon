package server

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- setFieldValue table-driven tests ---
func TestSetFieldValue(t *testing.T) {
	tests := []struct {
		name      string
		field     reflect.Value
		value     string
		wantVal   any
		expectErr bool
	}{
		{"string", reflect.ValueOf(new(string)).Elem(), "hello", "hello", false},
		{"int", reflect.ValueOf(new(int64)).Elem(), "42", int64(42), false},
		{"int_fail", reflect.ValueOf(new(int64)).Elem(), "notanint", nil, true},
		{"bool_true", reflect.ValueOf(new(bool)).Elem(), "true", true, false},
		{"bool_false", reflect.ValueOf(new(bool)).Elem(), "false", false, false},
		{"bool_fail", reflect.ValueOf(new(bool)).Elem(), "oops", nil, true},
		{"float64", reflect.ValueOf(new(float64)).Elem(), "3.14", 3.14, false},
		{"float_fail", reflect.ValueOf(new(float64)).Elem(), "nan", math.NaN(), false},
		{"unsupported", reflect.ValueOf(new([]string)).Elem(), "value", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Context{}
			err := c.setFieldValue(tt.field, tt.value)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.name == "float_fail" {
					fVal := tt.field.Interface().(float64)
					assert.True(t, math.IsNaN(fVal))
				} else {
					assert.Equal(t, tt.wantVal, tt.field.Interface())
				}
			}
		})
	}
}

func TestBindFormToStruct(t *testing.T) {
	type payload struct {
		Name  string  `form:"name"`
		Age   int     `form:"age"`
		Admin bool    `form:"admin"`
		Rate  float64 `form:"rate"`
		Skip  string  `form:"-"`
	}
	values := url.Values{
		"name":  {"Rishi"},
		"age":   {"21"},
		"admin": {"true"},
		"rate":  {"3.14"},
	}
	c := &Context{}
	var p payload
	err := c.bindFormToStruct(values, &p)
	assert.NoError(t, err)
	assert.Equal(t, "Rishi", p.Name)
	assert.Equal(t, 21, p.Age)
	assert.Equal(t, true, p.Admin)
	assert.Equal(t, 3.14, p.Rate)
	assert.Equal(t, "", p.Skip)
}

func TestShouldBindBody(t *testing.T) {
	t.Run("empty body", func(t *testing.T) {
		c := &Context{Request: &http.Request{Body: http.NoBody}, Writer: httptest.NewRecorder()}
		var dest any
		err := c.shouldBindBody(&dest, "application/json", json.Unmarshal)
		assert.Error(t, err)
	})

	t.Run("valid JSON", func(t *testing.T) {
		body := `{"name":"Rishi"}`
		c := &Context{Request: httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), Writer: httptest.NewRecorder()}
		c.Request.Header.Set("Content-Type", "application/json")
		var dest map[string]string
		err := c.shouldBindBody(&dest, "application/json", json.Unmarshal)
		assert.NoError(t, err)
		assert.Equal(t, "Rishi", dest["name"])
	})

	t.Run("content-type mismatch", func(t *testing.T) {
		body := `{"name":"Rishi"}`
		c := &Context{Request: httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), Writer: httptest.NewRecorder()}
		c.Request.Header.Set("Content-Type", "application/xml")
		var dest map[string]string
		err := c.shouldBindBody(&dest, "application/json", json.Unmarshal)
		assert.Error(t, err)
	})
}
