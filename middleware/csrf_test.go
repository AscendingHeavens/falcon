package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ascendingheavens/falcon/server"
	"github.com/stretchr/testify/assert"
)

func newCSRFTContext(method string, token string) *server.Context {
	req := httptest.NewRequest(method, "/", nil)
	if token != "" {
		req.Header.Set(defaultCSRFConfig.TokenHeader, token)
	}
	w := httptest.NewRecorder()
	return &server.Context{
		Request: req,
		Writer:  w,
	}
}

// withCSRFCookie adds the cookie a browser would attach on its own.
func withCSRFCookie(c *server.Context, value string) *server.Context {
	c.Request.AddCookie(&http.Cookie{Name: defaultCSRFConfig.TokenCookie, Value: value})
	return c
}

func csrfHandler(m Middleware, called *bool) server.HandlerFunc {
	return m(func(ctx *server.Context) *server.Response {
		*called = true
		return &server.Response{Success: true, Code: http.StatusOK}
	})
}

// issueCSRFToken does what a frontend does first: a GET, keeping the cookie and the token.
func issueCSRFToken(t *testing.T, m Middleware) (cookie, token string) {
	t.Helper()
	var called bool
	c := newCSRFTContext(http.MethodGet, "")
	csrfHandler(m, &called)(c)
	assert.True(t, called)

	for _, ck := range c.Writer.(*httptest.ResponseRecorder).Result().Cookies() {
		if ck.Name == defaultCSRFConfig.TokenCookie {
			cookie = ck.Value
		}
	}
	token, _ = c.Get(defaultCSRFConfig.ContextKey).(string)
	assert.NotEmpty(t, cookie, "a cookie must be issued")
	assert.NotEmpty(t, token, "a token must be available to the handler")
	return cookie, token
}

func TestCSRF_SkipSafeMethods(t *testing.T) {
	called := false
	c := newCSRFTContext(http.MethodGet, "")

	resp := csrfHandler(CSRF(), &called)(c)

	assert.True(t, called, "handler must be called for safe method")
	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestCSRF_SafeMethodIssuesCookieAndToken(t *testing.T) {
	var called bool
	c := newCSRFTContext(http.MethodGet, "")
	csrfHandler(CSRF(), &called)(c)

	token := c.Get(defaultCSRFConfig.ContextKey)
	assert.NotEmpty(t, token, "CSRF token should be available in context")

	cookies := c.Writer.(*httptest.ResponseRecorder).Result().Cookies()
	assert.Len(t, cookies, 1)
	ck := cookies[0]
	assert.Equal(t, defaultCSRFConfig.TokenCookie, ck.Name)
	assert.NotEqual(t, token, ck.Value, "the token must be derived from the cookie, not equal to it")
	assert.True(t, ck.HttpOnly)
	assert.True(t, ck.Secure)
	assert.Equal(t, http.SameSiteLaxMode, ck.SameSite)
}

func TestCSRF_ValidTokenPasses(t *testing.T) {
	m := CSRF()
	cookie, token := issueCSRFToken(t, m)

	called := false
	c := withCSRFCookie(newCSRFTContext(http.MethodPost, token), cookie)
	resp := csrfHandler(m, &called)(c)

	assert.True(t, called, "handler must be called with a valid token")
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, token, c.Get(defaultCSRFConfig.ContextKey), "the token must stay valid across requests")
}

func TestCSRF_Rejects(t *testing.T) {
	m := CSRF()
	cookie, token := issueCSRFToken(t, m)

	// A token from another server: right format, wrong secret.
	otherCookie, otherToken := issueCSRFToken(t, CSRF())

	tests := []struct {
		name   string
		token  string
		cookie string
	}{
		{"no token and no cookie", "", ""},
		{"forged request: the browser attaches the cookie, but there's no token", "", cookie},
		{"token without its cookie", token, ""},
		{"the cookie value replayed as the token", cookie, cookie},
		{"tampered token", token + "x", cookie},
		{"arbitrary token", "fake-token", cookie},
		{"valid token for a different cookie", token, otherCookie},
		{"attacker plants their own cookie and token", otherToken, otherCookie},
		{"attacker chooses the cookie value", "chosen", "chosen"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			c := newCSRFTContext(http.MethodPost, tt.token)
			if tt.cookie != "" {
				withCSRFCookie(c, tt.cookie)
			}

			resp := csrfHandler(m, &called)(c)

			assert.False(t, called, "handler must NOT be called")
			assert.Equal(t, http.StatusForbidden, resp.Code)
			assert.Contains(t, resp.Message, "invalid CSRF token")
		})
	}
}

func TestCSRF_RejectionIsAnEnvelope(t *testing.T) {
	var called bool
	c := newCSRFTContext(http.MethodPost, "")
	csrfHandler(CSRF(), &called)(c)

	rec := c.Writer.(*httptest.ResponseRecorder)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	var body server.Response
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.False(t, body.Success)
	assert.Equal(t, http.StatusForbidden, body.Code)
}

func TestCSRF_DefaultSecretIsRandom(t *testing.T) {
	cookie := "same-cookie"
	token := func(m Middleware) any {
		var called bool
		c := withCSRFCookie(newCSRFTContext(http.MethodGet, ""), cookie)
		csrfHandler(m, &called)(c)
		return c.Get(defaultCSRFConfig.ContextKey)
	}

	assert.NotEqual(t, token(CSRF()), token(CSRF()), "there must be no shared built-in secret")
}

func TestCSRF_SharedSecretWorksAcrossInstances(t *testing.T) {
	cfg := CSRFConfig{Secret: []byte("shared"), CookieSecure: true, CookieHTTPOnly: true}
	cookie, token := issueCSRFToken(t, CSRFWithConfig(cfg))

	called := false
	c := withCSRFCookie(newCSRFTContext(http.MethodPost, token), cookie)
	csrfHandler(CSRFWithConfig(cfg), &called)(c)

	assert.True(t, called)
}

func TestCSRF_ErrorHandlerIsUsed(t *testing.T) {
	var got error
	m := CSRFWithConfig(CSRFConfig{
		ErrorHandler: func(c *server.Context, err error) *server.Response {
			got = err
			return &server.Response{Message: "custom", Code: http.StatusTeapot}
		},
	})

	called := false
	resp := csrfHandler(m, &called)(newCSRFTContext(http.MethodPost, ""))

	assert.False(t, called)
	assert.True(t, errors.Is(got, ErrCSRFInvalid))
	assert.Equal(t, http.StatusTeapot, resp.Code)
}

func TestCSRF_CustomSkipMethods(t *testing.T) {
	m := CSRFWithConfig(CSRFConfig{SkipMethods: []string{http.MethodPost}})

	called := false
	csrfHandler(m, &called)(newCSRFTContext(http.MethodPost, ""))
	assert.True(t, called, "POST was explicitly skipped")

	called = false
	csrfHandler(m, &called)(newCSRFTContext(http.MethodGet, ""))
	assert.False(t, called, "GET is no longer skipped")
}
