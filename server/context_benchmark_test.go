package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

var (
	benchmarkContextSink *Context
	benchmarkBodySink    io.ReadCloser
)

type benchmarkBody struct {
	data   []byte
	offset int
}

func (b *benchmarkBody) Read(p []byte) (int, error) {
	if b.offset >= len(b.data) {
		return 0, io.EOF
	}

	n := copy(p, b.data[b.offset:])
	b.offset += n
	return n, nil
}

func (b *benchmarkBody) Close() error {
	return nil
}

func (b *benchmarkBody) Reset() {
	b.offset = 0
}

func resetWithoutBodyLimit(
	c *Context,
	w http.ResponseWriter,
	r *http.Request,
) {
	c.Writer = w
	c.Request = r
	c.Params = c.Params[:0]
	c.Handled = false
	c.Validator = nil
	c.Values = nil
	c.stream = nil
	c.bodyLimited = false
	c.query = nil
}

func BenchmarkContextResetBodyLimit(b *testing.B) {
	payload := []byte(`{"message":"hello world","data":"benchmark payload"}`)

	cfg := defaultSettings
	cfg.BodyLimit = 1 << 20

	b.Run("WithoutBodyLimit", func(b *testing.B) {
		c := NewContext(&cfg)
		writer := httptest.NewRecorder()

		body := &benchmarkBody{data: payload}
		req := &http.Request{
			Method: http.MethodPost,
			Body:   body,
		}

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			body.Reset()
			req.Body = body

			resetWithoutBodyLimit(c, writer, req)

			benchmarkContextSink = c
			benchmarkBodySink = c.Request.Body
		}
	})

	b.Run("WithBodyLimit", func(b *testing.B) {
		c := NewContext(&cfg)
		writer := httptest.NewRecorder()

		body := &benchmarkBody{data: payload}
		req := &http.Request{
			Method: http.MethodPost,
			Body:   body,
		}

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			body.Reset()
			req.Body = body

			c.Reset(writer, req)

			benchmarkContextSink = c
			benchmarkBodySink = c.Request.Body
		}
	})

	b.Run("WithoutBody", func(b *testing.B) {
		c := NewContext(&cfg)
		writer := httptest.NewRecorder()

		req := &http.Request{
			Method: http.MethodGet,
		}

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			c.Reset(writer, req)

			benchmarkContextSink = c
			benchmarkBodySink = c.Request.Body
		}
	})
}
