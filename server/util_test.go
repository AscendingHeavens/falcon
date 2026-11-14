package server

import (
	"encoding/json"
	"errors"
	"io"
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
		// Strings
		{"string", reflect.ValueOf(new(string)).Elem(), "hello", "hello", false},

		// Signed integers
		{"int", reflect.ValueOf(new(int)).Elem(), "42", int64(42), false},
		{"int8_valid", reflect.ValueOf(new(int8)).Elem(), "127", int64(127), false},
		{"int8_overflow", reflect.ValueOf(new(int8)).Elem(), "300", nil, true},
		{"int16_valid", reflect.ValueOf(new(int16)).Elem(), "32767", int64(32767), false},
		{"int16_overflow", reflect.ValueOf(new(int16)).Elem(), "32768", nil, true},
		{"int32_valid", reflect.ValueOf(new(int32)).Elem(), "2147483647", int64(2147483647), false},
		{"int32_overflow", reflect.ValueOf(new(int32)).Elem(), "2147483648", nil, true},
		{"int64", reflect.ValueOf(new(int64)).Elem(), "42", int64(42), false},
		{"negative_int", reflect.ValueOf(new(int)).Elem(), "-42", int64(-42), false},
		{"int_fail", reflect.ValueOf(new(int64)).Elem(), "notanint", nil, true},

		// Unsigned integers
		{"uint", reflect.ValueOf(new(uint)).Elem(), "42", uint64(42), false},
		{"uint8_valid", reflect.ValueOf(new(uint8)).Elem(), "255", uint64(255), false},
		{"uint8_overflow", reflect.ValueOf(new(uint8)).Elem(), "256", nil, true},
		{"uint16_valid", reflect.ValueOf(new(uint16)).Elem(), "65535", uint64(65535), false},
		{"uint16_overflow", reflect.ValueOf(new(uint16)).Elem(), "65536", nil, true},
		{"uint32_valid", reflect.ValueOf(new(uint32)).Elem(), "4294967295", uint64(4294967295), false},
		{"uint32_overflow", reflect.ValueOf(new(uint32)).Elem(), "4294967296", nil, true},
		{"uint64", reflect.ValueOf(new(uint64)).Elem(), "18446744073709551615", uint64(math.MaxUint64), false},
		{"uint_negative", reflect.ValueOf(new(uint)).Elem(), "-1", nil, true},
		{"uint_invalid", reflect.ValueOf(new(uint64)).Elem(), "notanumber", nil, true},

		// Booleans
		{"bool_true", reflect.ValueOf(new(bool)).Elem(), "true", true, false},
		{"bool_false", reflect.ValueOf(new(bool)).Elem(), "false", false, false},
		{"bool_fail", reflect.ValueOf(new(bool)).Elem(), "oops", nil, true},

		// Floating-point values
		{"float32", reflect.ValueOf(new(float32)).Elem(), "3.14", float64(float32(3.14)), false},
		{"float64", reflect.ValueOf(new(float64)).Elem(), "3.14", 3.14, false},
		{"float_fail", reflect.ValueOf(new(float64)).Elem(), "invalid", nil, true},
		{"float_nan", reflect.ValueOf(new(float64)).Elem(), "NaN", math.NaN(), false},

		// Unsupported types
		{"unsupported", reflect.ValueOf(new([]string)).Elem(), "value", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Context{}
			err := c.setFieldValue(tt.field, tt.value)

			if tt.expectErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)

			if tt.name == "float_nan" {
				fVal := tt.field.Interface().(float64)
				assert.True(t, math.IsNaN(fVal))
				return
			}

			switch tt.field.Kind() {
			case reflect.String:
				assert.Equal(t, tt.wantVal, tt.field.String())
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				assert.Equal(t, tt.wantVal, tt.field.Int())
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				assert.Equal(t, tt.wantVal, tt.field.Uint())
			case reflect.Bool:
				assert.Equal(t, tt.wantVal, tt.field.Bool())
			case reflect.Float32, reflect.Float64:
				assert.Equal(t, tt.wantVal, tt.field.Float())
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

func TestBindFormToStructIntegerOverflow(t *testing.T) {
	type payload struct {
		Age int8 `form:"age"`
	}

	values := url.Values{
		"age": {"300"},
	}

	c := &Context{}
	var p payload

	err := c.bindFormToStruct(values, &p)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Age")
}

func TestBindFormToStructUnsigned(t *testing.T) {
	type payload struct {
		UintValue   uint   `form:"uint"`
		Uint8Value  uint8  `form:"uint8"`
		Uint16Value uint16 `form:"uint16"`
		Uint32Value uint32 `form:"uint32"`
		Uint64Value uint64 `form:"uint64"`
	}

	values := url.Values{
		"uint":   {"100"},
		"uint8":  {"255"},
		"uint16": {"65535"},
		"uint32": {"4294967295"},
		"uint64": {"18446744073709551615"},
	}

	c := &Context{}
	var p payload

	err := c.bindFormToStruct(values, &p)

	assert.NoError(t, err)
	assert.Equal(t, uint(100), p.UintValue)
	assert.Equal(t, uint8(255), p.Uint8Value)
	assert.Equal(t, uint16(65535), p.Uint16Value)
	assert.Equal(t, uint32(4294967295), p.Uint32Value)
	assert.Equal(t, uint64(math.MaxUint64), p.Uint64Value)
}

func TestBindFormToStructUnsignedNegative(t *testing.T) {
	type payload struct {
		Count uint `form:"count"`
	}

	values := url.Values{
		"count": {"-1"},
	}

	c := &Context{}
	var p payload

	err := c.bindFormToStruct(values, &p)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Count")
}

func TestBindFormToStructInvalidDestination(t *testing.T) {
	c := &Context{}

	tests := []struct {
		name string
		dest any
	}{
		{"nil", nil},
		{"non-pointer", struct{}{}},
		{"pointer-to-int", new(int)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.bindFormToStruct(url.Values{}, tt.dest)
			assert.Error(t, err)
		})
	}
}

func TestShouldBindBody(t *testing.T) {
	t.Run("empty body", func(t *testing.T) {
		c := &Context{
			Request: &http.Request{Body: http.NoBody},
			Writer:  httptest.NewRecorder(),
		}

		var dest any
		err := c.shouldBindBody(&dest, "application/json", json.Unmarshal)

		assert.Error(t, err)
	})

	t.Run("valid JSON", func(t *testing.T) {
		body := `{"name":"Rishi"}`

		c := &Context{
			Request: httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)),
			Writer:  httptest.NewRecorder(),
		}
		c.Request.Header.Set("Content-Type", "application/json")

		var dest map[string]string
		err := c.shouldBindBody(&dest, "application/json", json.Unmarshal)

		assert.NoError(t, err)
		assert.Equal(t, "Rishi", dest["name"])
	})

	t.Run("content-type mismatch", func(t *testing.T) {
		body := `{"name":"Rishi"}`

		c := &Context{
			Request: httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)),
			Writer:  httptest.NewRecorder(),
		}
		c.Request.Header.Set("Content-Type", "application/xml")

		var dest map[string]string
		err := c.shouldBindBody(&dest, "application/json", json.Unmarshal)

		assert.Error(t, err)
	})

	t.Run("invalid content type", func(t *testing.T) {
		c := &Context{
			Request: httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}")),
			Writer:  httptest.NewRecorder(),
		}
		c.Request.Header.Set("Content-Type", "invalid; charset")

		var dest map[string]string
		err := c.shouldBindBody(&dest, "application/json", json.Unmarshal)

		assert.Error(t, err)
	})

	t.Run("body exceeds limit", func(t *testing.T) {
		settings := &Settings{
			BodyLimit: 8,
		}

		c := NewContext(settings)
		req := httptest.NewRequest(
			http.MethodPost,
			"/",
			strings.NewReader(`{"name":"Rishi"}`),
		)
		req.Header.Set("Content-Type", "application/json")

		c.Reset(httptest.NewRecorder(), req)

		var dest map[string]string
		err := c.shouldBindBody(&dest, "application/json", json.Unmarshal)

		assert.Error(t, err)

		var maxErr *http.MaxBytesError
		assert.True(t, errors.As(err, &maxErr))
	})
}

// --- Request body limit tests ---

func TestContextBodyLimit(t *testing.T) {
	const bodyLimit = 8

	t.Run("body within limit", func(t *testing.T) {
		body := "12345678"

		ctx := NewContext(&Settings{BodyLimit: bodyLimit})
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))

		ctx.Reset(httptest.NewRecorder(), req)

		data, err := io.ReadAll(ctx.Request.Body)

		assert.NoError(t, err)
		assert.Equal(t, body, string(data))
	})

	t.Run("body exceeds limit", func(t *testing.T) {
		body := "123456789"

		ctx := NewContext(&Settings{BodyLimit: bodyLimit})
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))

		ctx.Reset(httptest.NewRecorder(), req)

		data, err := io.ReadAll(ctx.Request.Body)

		assert.Error(t, err)

		var maxErr *http.MaxBytesError
		assert.True(t, errors.As(err, &maxErr))

		assert.Len(t, data, bodyLimit)
	})
}

// TestContextBodyLimitReset verifies that a reused Context applies
// the body limit independently to each incoming request.
func TestContextBodyLimitReset(t *testing.T) {
	ctx := NewContext(&Settings{BodyLimit: 8})

	firstReq := httptest.NewRequest(
		http.MethodPost,
		"/first",
		strings.NewReader("123456789"),
	)

	ctx.Reset(httptest.NewRecorder(), firstReq)

	_, firstErr := io.ReadAll(ctx.Request.Body)

	var firstMaxErr *http.MaxBytesError
	assert.Error(t, firstErr)
	assert.True(t, errors.As(firstErr, &firstMaxErr))

	// Reuse the same Context with another request.
	secondReq := httptest.NewRequest(
		http.MethodPost,
		"/second",
		strings.NewReader("abcdefgh"),
	)

	ctx.Reset(httptest.NewRecorder(), secondReq)

	data, secondErr := io.ReadAll(ctx.Request.Body)

	assert.NoError(t, secondErr)
	assert.Equal(t, "abcdefgh", string(data))
}
