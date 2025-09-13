package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResponseHelpers(t *testing.T) {
	t.Run("String writes plain text", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c := &Context{Writer: rec}
		resp := c.String(200, "Hello")
		assert.Equal(t, 200, rec.Code)
		assert.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
		assert.Equal(t, "Hello", rec.Body.String())
		assert.True(t, c.Handled)
		assert.Equal(t, "Hello", resp.Message)
	})

	t.Run("HTML writes HTML content", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c := &Context{Writer: rec}
		resp := c.HTML(200, "<h1>Hi</h1>")
		assert.Equal(t, 200, rec.Code)
		assert.Equal(t, "text/html", rec.Header().Get("Content-Type"))
		assert.Equal(t, "<h1>Hi</h1>", rec.Body.String())
		assert.True(t, c.Handled)
		assert.Equal(t, "HTML written", resp.Message)
	})

	t.Run("Blob writes raw data", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c := &Context{Writer: rec}
		data := []byte{1, 2, 3}
		resp := c.Blob(200, data, "application/octet-stream")
		assert.Equal(t, 200, rec.Code)
		assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
		assert.Equal(t, data, rec.Body.Bytes())
		assert.True(t, c.Handled)
		assert.Equal(t, "Blob written", resp.Message)
	})

	t.Run("JSON writes response object", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c := &Context{Writer: rec}
		r := &Response{Success: true, Message: "ok", Code: 200}
		resp := c.JSON(true, "ok", nil, 200)
		assert.Equal(t, 200, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		assert.JSONEq(t, `{"success":true,"message":"ok","code":200}`, rec.Body.String())
		assert.True(t, c.Handled)
		assert.Equal(t, r, resp)
	})

	t.Run("Redirect sets location", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c := &Context{Writer: rec}
		resp := c.Redirect(302, "/login")
		assert.Equal(t, 302, rec.Code)
		assert.Equal(t, "/login", rec.Header().Get("Location"))
		assert.True(t, c.Handled)
		assert.Equal(t, "Redirected to /login", resp.Message)
	})

	t.Run("File serves existing file", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c := &Context{Writer: rec}

		// create temporary file
		filePath := t.TempDir() + "/test.txt"
		content := []byte("hello file")
		if err := os.WriteFile(filePath, content, 0644); err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}

		resp := c.File(filePath)
		assert.Equal(t, 200, rec.Code)
		assert.Equal(t, http.DetectContentType(content), rec.Header().Get("Content-Type"))
		assert.Equal(t, content, rec.Body.Bytes())
		assert.True(t, c.Handled)
		assert.Contains(t, resp.Message, "Served file")
	})

	t.Run("File returns 404 if missing", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c := &Context{Writer: rec}
		resp := c.File("/nonexistent.file")
		assert.Equal(t, 404, rec.Code)
		assert.True(t, c.Handled)
		assert.Equal(t, "File not found", resp.Message)
	})
}

func TestErrorJSON(t *testing.T) {
	t.Run("Writes JSON response and sets handled", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c := &Context{Writer: rec}

		resp := c.ErrorJSON("Something went wrong", map[string]string{"field": "value"}, 400)

		// Check that c.Handled is set
		assert.True(t, c.Handled)

		// Check that returned Response object matches
		assert.False(t, resp.Success)
		assert.Equal(t, "Something went wrong", resp.Message)
		assert.Equal(t, 400, resp.Code)
		assert.Equal(t, map[string]string{"field": "value"}, resp.Details)

		// Check the actual HTTP response
		assert.Equal(t, 400, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		// Decode JSON written to ResponseWriter
		// Decode JSON written to ResponseWriter
		var body Response
		err := json.NewDecoder(rec.Body).Decode(&body)
		assert.NoError(t, err)
		expectedDetails := map[string]interface{}{"field": "value"}
		assert.Equal(t, expectedDetails, body.Details) // fixed
		assert.Equal(t, resp.Message, body.Message)
		assert.Equal(t, resp.Code, body.Code)
		assert.Equal(t, resp.Success, body.Success)

	})

	t.Run("Does not write twice if already handled", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c := &Context{Writer: rec, Handled: true}

		resp := c.ErrorJSON("Already handled", nil, 500)

		// Should return Response but not write to writer
		assert.True(t, c.Handled)
		assert.Equal(t, "Already handled", resp.Message)
		assert.Equal(t, 500, resp.Code)
		assert.Equal(t, 0, rec.Body.Len()) // nothing written
	})
}
