package falcon

import (
	"fmt"
	"time"

	"github.com/ascendingheavens/falcon/server"
)

// Option configures a Server. Pass options to New.
// Example: app := falcon.New(falcon.WithDebug(), falcon.WithBodyLimit(1<<20))
type Option func(*Server)

// Timeouts are the limits of the underlying http.Server. Zero means no limit.
type Timeouts struct {
	ReadHeader time.Duration // Reading the request headers. This is what stops slowloris attacks
	Read       time.Duration // Reading the whole request, including its body
	Write      time.Duration // Writing the response. c.File and c.Stream are exempt
	Idle       time.Duration // Keeping a connection open between requests
}

// defaultTimeouts keeps slow or stalled clients from holding connections open forever.
var defaultTimeouts = Timeouts{
	ReadHeader: 10 * time.Second,
	Read:       60 * time.Second,
	Write:      60 * time.Second,
	Idle:       120 * time.Second,
}

// WithDebug makes error responses include the underlying error, which helps during
// development but exposes internals. Never use it in production.
func WithDebug() Option {
	return func(s *Server) {
		s.settings.Debug = true
	}
}

// WithErrorMapper sets a function that turns your application's errors into responses
// in one place, so that handlers only need to call c.Error. It runs before any other
// handling; return nil to leave an error to the default behaviour.
//
// Example:
//
//	falcon.WithErrorMapper(func(err error) *falcon.Response {
//		if errors.Is(err, repo.ErrNotFound) {
//			return &falcon.Response{Message: "Not found", Code: 404}
//		}
//		return nil
//	})
func WithErrorMapper(mapper func(err error) *Response) Option {
	return func(s *Server) {
		s.settings.ErrorMapper = mapper
	}
}

// WithBodyLimit sets the maximum size of a request body in bytes, including file uploads.
// Larger bodies get a 413. The default is 10MB, so raise it if you accept large uploads.
func WithBodyLimit(bytes int64) Option {
	if bytes <= 0 {
		panic(fmt.Sprintf("falcon: WithBodyLimit needs a positive size, got %d", bytes))
	}
	return func(s *Server) {
		s.settings.BodyLimit = bytes
	}
}

// WithMultipartMemory sets how many bytes of a multipart form are kept in memory;
// the rest is stored in temporary files. The default is 32MB.
func WithMultipartMemory(bytes int64) Option {
	if bytes <= 0 {
		panic(fmt.Sprintf("falcon: WithMultipartMemory needs a positive size, got %d", bytes))
	}
	return func(s *Server) {
		s.settings.MultipartMemory = bytes
	}
}

// WithTimeouts replaces the server's timeouts. Any limit left at zero is turned off.
// The defaults are 10s to read headers, 60s to read a request, 60s to write a
// response, and 120s for an idle connection.
func WithTimeouts(timeouts Timeouts) Option {
	return func(s *Server) {
		s.timeouts = timeouts
	}
}

// NewHTTPError creates an error that c.Error turns into a response with this code and message.
// Example: return c.Error(falcon.NewHTTPError(403, "You can't edit this post"))
func NewHTTPError(code int, message string) *HTTPError {
	return server.NewHTTPError(code, message)
}
