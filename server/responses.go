package server

import (
	"errors"
	"io"
	"net/http"
	"os"
)

// String returns a plain text response.
// Example: return c.String(200, "Hello World")
func (c *Context) String(code int, s string) *Response {
	return rawResponse(code, &rawBody{contentType: "text/plain", data: []byte(s)})
}

// HTML returns an HTML response.
// Example: return c.HTML(200, "<h1>Hello</h1>")
func (c *Context) HTML(code int, html string) *Response {
	return rawResponse(code, &rawBody{contentType: "text/html", data: []byte(html)})
}

// Blob returns a response with raw binary data and the given Content-Type.
// Example: return c.Blob(200, data, "image/png")
func (c *Context) Blob(code int, data []byte, contentType string) *Response {
	return rawResponse(code, &rawBody{contentType: contentType, data: data})
}

// Stream returns a response whose body is copied from r as it's sent, without holding
// it all in memory. Use it for large or generated bodies, such as exports or proxying.
// If r is an io.Closer it's closed afterwards, even if middleware replaced the response.
// The server's write timeout doesn't apply to a stream.
// Example: return c.Stream(200, "text/csv", exportReader)
func (c *Context) Stream(code int, contentType string, r io.Reader) *Response {
	c.closeStream()
	if closer, ok := r.(io.Closer); ok {
		c.stream = closer
	}
	return rawResponse(code, &rawBody{contentType: contentType, reader: r})
}

// File returns a response that serves a file from disk, with support for range
// requests and caching. If the file doesn't exist, it returns a 404 instead.
// The path is used as given, so never build it directly from user input.
// Example: return c.File("./reports/latest.pdf")
func (c *Context) File(filePath string) *Response {
	info, err := os.Stat(filePath)
	if err != nil {
		return c.fileNotFound(err)
	}
	if info.IsDir() {
		return c.fileNotFound(errors.New(filePath + " is a directory"))
	}
	return rawResponse(http.StatusOK, &rawBody{file: filePath})
}

// fileNotFound returns the 404 for a file that couldn't be served.
func (c *Context) fileNotFound(err error) *Response {
	return c.Error(&HTTPError{Code: http.StatusNotFound, Message: "File not found", Err: err})
}

// Redirect returns an HTTP redirect to the specified location.
// Example: return c.Redirect(302, "/login")
func (c *Context) Redirect(code int, location string) *Response {
	return rawResponse(code, &rawBody{location: location})
}

// NoContent returns a 204 response with no body.
func (c *Context) NoContent() *Response {
	return rawResponse(http.StatusNoContent, &rawBody{})
}

// rawResponse returns a response sent as something other than the JSON envelope.
func rawResponse(code int, raw *rawBody) *Response {
	return &Response{Success: code < http.StatusBadRequest, Code: code, raw: raw}
}

// OK returns a successful 200 response.
// Example: return c.OK("User found", user)
func (c *Context) OK(message string, details any) *Response {
	return &Response{Success: true, Message: message, Details: details, Code: http.StatusOK}
}

// Created returns a successful 201 response.
// Example: return c.Created("User created", user)
func (c *Context) Created(message string, details any) *Response {
	return &Response{Success: true, Message: message, Details: details, Code: http.StatusCreated}
}

// JSON returns a response with every field of the envelope given explicitly.
// For the common cases see OK and Created for success, and Error for failure.
func (c *Context) JSON(success bool, message string, details any, code int) *Response {
	return &Response{Success: success, Message: message, Details: details, Code: code}
}

// ErrorJSON returns a failure response. To turn an existing error into a response, use Error.
func (c *Context) ErrorJSON(message string, details any, code int) *Response {
	return &Response{Success: false, Message: message, Details: details, Code: code}
}
