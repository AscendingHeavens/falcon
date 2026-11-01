package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// maxPooledBuffer is the largest buffer kept for reuse, so that one huge
// response doesn't pin its memory for the life of the process.
const maxPooledBuffer = 64 << 10

// jsonContentType is shared by every JSON response, so that setting the header
// doesn't allocate. Its capacity is 1, so appending to it can't change it for others.
var jsonContentType = []string{"application/json"}

// envelopeWriter builds the JSON envelope. The encoder writes into buf, and is kept
// with it because creating an encoder for every response would allocate.
type envelopeWriter struct {
	buf     bytes.Buffer
	encoder *json.Encoder
}

var envelopeWriters = sync.Pool{
	New: func() any {
		w := &envelopeWriter{}
		w.encoder = json.NewEncoder(&w.buf)
		return w
	},
}

// WriteResponse sends a Response to the client. The server calls it once the handler
// and all middleware have returned. It does nothing if the handler took over the
// Writer (c.Handled) or the response has already been written.
func (c *Context) WriteResponse(resp *Response) {
	if !c.Handled {
		if resp == nil {
			// Handlers are guarded, so this came from a middleware.
			resp = c.internalError(errors.New("a middleware returned a nil Response"))
		}
		c.send(resp)
	}
	if c.stream != nil {
		c.closeStream()
	}
}

// send writes a response that hasn't been written yet.
func (c *Context) send(resp *Response) {
	c.Handled = true

	// net/http panics on a status code outside this range.
	if resp.Code < 100 || resp.Code > 999 {
		resp = c.internalError(fmt.Errorf("invalid status code %d in response %q", resp.Code, resp.Message))
	}

	if resp.raw != nil {
		c.writeRaw(resp)
		return
	}
	c.writeEnvelope(resp)
}

// closeStream closes the reader given to c.Stream. It runs whichever response ended up
// being sent, because middleware may have replaced the one that held the reader.
func (c *Context) closeStream() {
	if c.stream != nil {
		_ = c.stream.Close()
		c.stream = nil
	}
}

// writeEnvelope sends a response as the JSON envelope. The body is built before anything
// is written, so a Details value that can't be encoded still gets a clean 500.
func (c *Context) writeEnvelope(resp *Response) {
	w := envelopeWriters.Get().(*envelopeWriter)
	defer func() {
		if w.buf.Cap() <= maxPooledBuffer {
			w.buf.Reset()
			envelopeWriters.Put(w)
		}
	}()

	if err := w.encode(resp); err != nil {
		resp = c.internalError(fmt.Errorf("encoding the Details of response %q: %w", resp.Message, err))
		w.buf.Reset()
		_ = w.encode(resp) // Its Details is nil or a string, which can't fail
	}

	c.Writer.Header()["Content-Type"] = jsonContentType
	c.Writer.WriteHeader(resp.Code)
	if bodyAllowed(resp.Code) {
		_, _ = c.Writer.Write(w.buf.Bytes())
	}
}

// encode writes a response into the buffer. Its output is identical to
// json.Encoder.Encode(resp), but the fixed parts of the envelope skip reflection.
func (w *envelopeWriter) encode(resp *Response) error {
	if resp.Success {
		w.buf.WriteString(`{"success":true,"message":`)
	} else {
		w.buf.WriteString(`{"success":false,"message":`)
	}
	if err := w.encodeString(resp.Message); err != nil {
		return err
	}

	if resp.Details != nil {
		w.buf.WriteString(`,"details":`)
		if err := w.encodeValue(resp.Details); err != nil {
			return err
		}
	}

	w.buf.WriteString(`,"code":`)
	w.buf.Write(strconv.AppendInt(w.buf.AvailableBuffer(), int64(resp.Code), 10))
	w.buf.WriteString("}\n")
	return nil
}

// encodeString writes a JSON string. Most messages are plain ASCII and can be
// copied as they are; anything else is left to encoding/json to escape.
func (w *envelopeWriter) encodeString(s string) error {
	for i := 0; i < len(s); i++ {
		if b := s[i]; b < 0x20 || b >= 0x80 || b == '"' || b == '\\' || b == '<' || b == '>' || b == '&' {
			return w.encodeValue(s)
		}
	}
	w.buf.WriteByte('"')
	w.buf.WriteString(s)
	w.buf.WriteByte('"')
	return nil
}

// encodeValue writes any value with encoding/json, without the newline Encode adds.
func (w *envelopeWriter) encodeValue(v any) error {
	if err := w.encoder.Encode(v); err != nil {
		return err
	}
	w.buf.Truncate(w.buf.Len() - 1)
	return nil
}

// bodyAllowed reports whether a response with this status code may have a body.
func bodyAllowed(code int) bool {
	return code >= 200 && code != http.StatusNoContent && code != http.StatusNotModified
}

// writeRaw sends a response that isn't the JSON envelope.
func (c *Context) writeRaw(resp *Response) {
	raw := resp.raw
	if raw.contentType != "" {
		c.Writer.Header().Set("Content-Type", raw.contentType)
	}
	if raw.location != "" {
		c.Writer.Header().Set("Location", raw.location)
	}

	switch {
	case raw.file != "":
		c.serveFile(raw.file)

	case raw.reader != nil:
		c.clearWriteDeadline()
		c.Writer.WriteHeader(resp.Code)
		if _, err := io.Copy(c.Writer, raw.reader); err != nil {
			c.logError(fmt.Errorf("streaming the response: %w", err))
		}

	default:
		c.Writer.WriteHeader(resp.Code)
		if len(raw.data) > 0 && bodyAllowed(resp.Code) {
			_, _ = c.Writer.Write(raw.data)
		}
	}
}

// serveFile streams a file from disk, with support for range requests and caching.
func (c *Context) serveFile(path string) {
	f, err := os.Open(path)
	if err == nil {
		defer f.Close()
	}

	var info os.FileInfo
	if err == nil {
		info, err = f.Stat()
	}
	if err != nil {
		// The file existed when c.File checked, but has gone since.
		c.writeEnvelope(c.fileNotFound(err))
		return
	}

	c.clearWriteDeadline()
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), f)
}

// clearWriteDeadline removes the server's write timeout for this response, which is
// meant for ordinary responses and would cut off a long download halfway.
func (c *Context) clearWriteDeadline() {
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{})
}
