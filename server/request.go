package server

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

// Bind reads the request body and decodes it into dest based on Content-Type.
// If binding fails it returns an *HTTPError, ready to pass to c.Error, with a message
// that's safe to show to clients. Use ShouldBind for the plain underlying error.
//
// Example:
//
//	if err := c.Bind(&req); err != nil {
//		return c.Error(err)
//	}
func (c *Context) Bind(dest any) error {
	return bindError("Invalid request body", c.ShouldBind(dest))
}

// BindJSON enforces JSON Content-Type and decodes into dest.
// If binding fails it returns an *HTTPError, like Bind.
func (c *Context) BindJSON(dest any) error {
	return bindError("Invalid JSON body", c.ShouldBindJSON(dest))
}

// BindXML enforces XML Content-Type and decodes into dest.
// If binding fails it returns an *HTTPError, like Bind.
func (c *Context) BindXML(dest any) error {
	return bindError("Invalid XML body", c.ShouldBindXML(dest))
}

// bindError turns a binding failure into an *HTTPError. It's nil if err is nil.
func bindError(message string, err error) error {
	if err == nil {
		return nil
	}

	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return &HTTPError{Code: http.StatusRequestEntityTooLarge, Message: "Request body too large", Err: err}
	}
	return &HTTPError{Code: http.StatusBadRequest, Message: message, Err: err}
}

// ShouldBind attempts to decode the request body into dest based on Content-Type
// and returns the underlying error if it fails.
func (c *Context) ShouldBind(dest any) error {
	contentType := c.Request.Header.Get("Content-Type")
	if contentType == "" {
		return errors.New("missing Content-Type header")
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fmt.Errorf("invalid Content-Type: %w", err)
	}

	switch mediaType {
	case "application/json":
		return c.ShouldBindJSON(dest)
	case "application/xml", "text/xml":
		return c.ShouldBindXML(dest)
	case "application/x-www-form-urlencoded":
		return c.ShouldBindForm(dest)
	case "multipart/form-data":
		return c.ShouldBindMultipart(dest)
	default:
		return fmt.Errorf("unsupported Content-Type: %s", mediaType)
	}
}

// ShouldBindJSON decodes JSON without automatic error response.
func (c *Context) ShouldBindJSON(dest any) error {
	return c.shouldBindBody(dest, "application/json", json.Unmarshal)
}

// ShouldBindXML decodes XML without automatic error response.
func (c *Context) ShouldBindXML(dest any) error {
	return c.shouldBindBody(dest, "application/xml", xml.Unmarshal)
}

// ShouldBindForm binds application/x-www-form-urlencoded form values into a struct.
func (c *Context) ShouldBindForm(dest any) error {
	c.limitBody()
	if err := c.Request.ParseForm(); err != nil {
		return fmt.Errorf("failed to parse form: %w", err)
	}
	if len(c.Request.PostForm) == 0 {
		return errors.New("no form values found")
	}
	return c.bindFormToStruct(c.Request.PostForm, dest)
}

// ShouldBindMultipart binds multipart form to struct.
func (c *Context) ShouldBindMultipart(dest any) error {
	if err := c.parseMultipart(); err != nil {
		return fmt.Errorf("failed to parse multipart form: %w", err)
	}
	return c.bindFormToStruct(c.Request.MultipartForm.Value, dest)
}

// Param returns the value of a path parameter by name.
// Example: /users/:id -> c.Param("id") returns "123"
func (c *Context) Param(name string) string {
	return c.Params.Get(name)
}

// Query returns the first value of a URL query parameter by key.
// Example: /search?q=golang -> c.Query("q") returns "golang"
func (c *Context) Query(key string) string {
	if c.Request == nil {
		return ""
	}
	if c.query != nil {
		return c.query.Get(key)
	}

	// Long queries go through net/url itself, so its limit on how many parameters it
	// will parse (set with GODEBUG=urlmaxqueryparams) applies exactly as it would.
	raw := c.Request.URL.RawQuery
	if strings.Count(raw, "&") >= maxScannedQueryParams {
		return c.queryValues().Get(key)
	}
	return rawQueryParam(raw, key)
}

// maxScannedQueryParams is how many parameters Query looks through itself before leaving
// the query to net/url. It's far below net/url's own default limit of 10,000.
const maxScannedQueryParams = 100

// rawQueryParam returns the first value of name in a raw query, exactly as url.ParseQuery
// followed by Get would, but without building the map: reading a few parameters this way
// doesn't allocate unless a value needs unescaping. Like ParseQuery, it skips pairs
// containing a semicolon or a malformed escape, so a later pair with the same name counts.
func rawQueryParam(query, name string) string {
	for query != "" {
		var pair string
		pair, query, _ = strings.Cut(query, "&")
		if pair == "" || strings.Contains(pair, ";") {
			continue
		}

		key, value, _ := strings.Cut(pair, "=")
		if key, err := unescapeQuery(key); err != nil || key != name {
			continue
		}
		if value, err := unescapeQuery(value); err == nil {
			return value
		}
	}
	return ""
}

// unescapeQuery is url.QueryUnescape, skipping the work for the common case of nothing to unescape.
func unescapeQuery(s string) (string, error) {
	if !strings.ContainsAny(s, "%+") {
		return s, nil
	}
	return url.QueryUnescape(s)
}

// queryValues returns the parsed query string. It's parsed at most once per request,
// so changing the request's RawQuery after reading a query value has no effect.
func (c *Context) queryValues() url.Values {
	if c.query == nil && c.Request.URL.RawQuery != "" {
		c.query = c.Request.URL.Query()
	}
	return c.query
}

// QueryArray returns all values for a query parameter key.
// Example: /filter?tag=go&tag=web -> c.QueryArray("tag") returns []string{"go", "web"}
func (c *Context) QueryArray(key string) []string {
	if c.Request == nil {
		return nil
	}
	return c.queryValues()[key]
}

// FormFile retrieves the file from a multipart form.
// It returns the first file for the provided form key.
// A request over the server's body limit gives an error that c.Error turns into a 413;
// raise the limit with falcon.WithBodyLimit if you accept large uploads.
// Example: <input type="file" name="avatar" /> -> c.FormFile("avatar")
func (c *Context) FormFile(name string) (multipart.File, *multipart.FileHeader, error) {
	if err := c.parseMultipart(); err != nil {
		return nil, nil, err
	}
	return c.Request.FormFile(name)
}

// FormValue returns the first value for the named component of the POST or PUT request body.
// It calls ParseMultipartForm and ParseForm if necessary. It can't report errors, so a
// request over the server's body limit gives "".
// Example: <input type="text" name="username" /> -> c.FormValue("username")
func (c *Context) FormValue(name string) string {
	_ = c.parseMultipart() // Fails for anything not multipart, which FormValue handles itself
	return c.Request.FormValue(name)
}

// PostFormValue is like FormValue but only reads the request body, ignoring the URL query.
// Bodies that aren't forms, such as JSON, are left unread for the handler.
func (c *Context) PostFormValue(name string) string {
	_ = c.parseMultipart() // Fails for anything not multipart, which PostFormValue handles itself
	return c.Request.PostFormValue(name)
}

// BindForm parses form data and binds it to a map
// Takes only the first value for each key
func (c *Context) BindForm(dest map[string]string) error {
	// Check if content type is form URL-encoded (allowing charset parameter)
	contentType := c.Request.Header.Get("Content-Type")
	if contentType == "" {
		return fmt.Errorf("missing content type header")
	}

	// Parse media type to handle charset parameter
	mediaType := strings.ToLower(strings.Split(contentType, ";")[0])
	if strings.TrimSpace(mediaType) != "application/x-www-form-urlencoded" {
		return fmt.Errorf("invalid content type, expected application/x-www-form-urlencoded, got %s", mediaType)
	}

	// Parse the form (this handles both POST body and URL query parameters)
	c.limitBody()
	if err := c.Request.ParseForm(); err != nil {
		return fmt.Errorf("failed to parse form: %w", err)
	}

	// Copy form values into dest map (first value only)
	for key, values := range c.Request.PostForm {
		if len(values) > 0 {
			dest[key] = values[0]
		}
	}

	return nil
}

// BindFormAll parses URL-encoded form data and returns all values in a url.Values map.
func (c *Context) BindFormAll() (url.Values, error) {
	// Check content type
	contentType := c.Request.Header.Get("Content-Type")
	if contentType == "" {
		return nil, fmt.Errorf("missing content type header")
	}

	mediaType := strings.ToLower(strings.Split(contentType, ";")[0])
	if strings.TrimSpace(mediaType) != "application/x-www-form-urlencoded" {
		return nil, fmt.Errorf("invalid content type, expected application/x-www-form-urlencoded, got %s", mediaType)
	}

	// Parse the form
	c.limitBody()
	if err := c.Request.ParseForm(); err != nil {
		return nil, fmt.Errorf("failed to parse form: %w", err)
	}

	// Return all form values
	return c.Request.PostForm, nil
}
