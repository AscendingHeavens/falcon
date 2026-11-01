package middleware

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"

	"github.com/ascendingheavens/falcon/server"
)

// generateCSRFToken creates a random token
func generateCSRFToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// readCSRFCookie returns the server token from the request's cookie, or "" if it has none
func readCSRFCookie(c *server.Context, cfg CSRFConfig) string {
	cookie, err := c.Request.Cookie(cfg.TokenCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// signCSRFToken derives the token a client must send from the server token
func signCSRFToken(secret []byte, serverToken string) string {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(serverToken))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// validateCSRFToken compares HMACed tokens in constant time
func validateCSRFToken(secret []byte, serverToken, clientToken string) bool {
	expected := signCSRFToken(secret, serverToken)
	return hmac.Equal([]byte(expected), []byte(clientToken))
}
