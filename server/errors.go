package server

import (
	"errors"
	"fmt"
	"log"
	"net/http"
)

// HTTPError is an error that knows which response it should become.
// Message and Details are sent to the client. Err is the underlying cause: it's
// logged on 5xx errors, and only sent to the client in debug mode.
//
// Example: return c.Error(falcon.NewHTTPError(403, "You can't edit this post"))
type HTTPError struct {
	Code    int
	Message string
	Details any
	Err     error
}

// NewHTTPError creates an HTTPError. An empty message means the standard text for the code.
func NewHTTPError(code int, message string) *HTTPError {
	return &HTTPError{Code: code, Message: message}
}

// Error implements the error interface.
func (e *HTTPError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%d %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%d %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause, for errors.Is and errors.As.
func (e *HTTPError) Unwrap() error {
	return e.Err
}

// Error turns any error into a response. It tries, in order:
//  1. the server's ErrorMapper, set with falcon.WithErrorMapper;
//  2. a *ValidationError, which becomes a 422 listing the invalid fields;
//  3. an *HTTPError, which becomes a response with its code, message and details;
//  4. anything else, which becomes a generic 500. The real error is logged, and
//     only sent to the client in debug mode.
//
// Example:
//
//	user, err := repo.Find(id)
//	if err != nil {
//		return c.Error(err)
//	}
func (c *Context) Error(err error) *Response {
	if err == nil {
		err = errors.New("c.Error called with a nil error")
	}

	settings := c.config()
	if settings.ErrorMapper != nil {
		if resp := settings.ErrorMapper(err); resp != nil {
			return resp
		}
	}

	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		return &Response{
			Message: "Validation failed",
			Details: validationErr.Fields,
			Code:    http.StatusUnprocessableEntity,
		}
	}

	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.Code >= http.StatusInternalServerError {
			c.logError(err)
		}

		message := httpErr.Message
		if message == "" {
			message = http.StatusText(httpErr.Code)
		}
		details := httpErr.Details
		if details == nil && settings.Debug && httpErr.Err != nil {
			details = httpErr.Err.Error()
		}
		return &Response{Message: message, Details: details, Code: httpErr.Code}
	}

	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return &Response{Message: "Request body too large", Code: http.StatusRequestEntityTooLarge}
	}

	return c.internalError(err)
}

// internalError logs err and returns a generic 500, with the error's text only in
// debug mode. It doesn't involve the ErrorMapper, because it's also how the server
// reports its own failures, and those can't depend on user code behaving.
func (c *Context) internalError(err error) *Response {
	c.logError(err)
	resp := &Response{
		Message: http.StatusText(http.StatusInternalServerError),
		Code:    http.StatusInternalServerError,
	}
	if c.config().Debug {
		resp.Details = err.Error()
	}
	return resp
}

// logError logs an error along with the request it happened on. The path is quoted
// because it comes from the client, and a newline in it could forge a log entry.
func (c *Context) logError(err error) {
	if c.Request == nil {
		log.Printf("falcon: %v", err)
		return
	}
	log.Printf("falcon: %s %q: %v", c.Request.Method, c.Request.URL.Path, err)
}

// config returns the server's settings, or the defaults if there's no server.
func (c *Context) config() *Settings {
	if c.settings == nil {
		return &defaultSettings
	}
	return c.settings
}
