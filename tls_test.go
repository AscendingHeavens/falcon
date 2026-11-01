package falcon

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// mockListenAndServeTLS replaces the real TLS startup for the duration of a test.
func mockListenAndServeTLS(t *testing.T, mock func(*http.Server, string, string) error) {
	t.Helper()
	original := listenAndServeTLS
	listenAndServeTLS = mock
	t.Cleanup(func() { listenAndServeTLS = original })
}

func TestStartTLS_CallsListenAndServeTLS(t *testing.T) {
	var gotServer *http.Server
	var cert, key string
	mockListenAndServeTLS(t, func(srv *http.Server, c, k string) error {
		gotServer, cert, key = srv, c, k
		return nil
	})

	srv := New()
	err := srv.StartTLS("127.0.0.1:8443", "cert.pem", "key.pem")

	assert.NoError(t, err)
	assert.Equal(t, "127.0.0.1:8443", gotServer.Addr)
	assert.Equal(t, srv, gotServer.Handler)
	assert.Equal(t, "cert.pem", cert)
	assert.Equal(t, "key.pem", key)
	assert.Equal(t, defaultTimeouts.ReadHeader, gotServer.ReadHeaderTimeout)
}

func TestStartTLS_ReturnsTheError(t *testing.T) {
	tlsErr := errors.New("tls error")
	mockListenAndServeTLS(t, func(*http.Server, string, string) error { return tlsErr })

	assert.ErrorIs(t, New().StartTLS("127.0.0.1:8443", "cert.pem", "key.pem"), tlsErr)
}

func TestStartTLS_ShutdownIsNotAnError(t *testing.T) {
	mockListenAndServeTLS(t, func(*http.Server, string, string) error { return http.ErrServerClosed })

	assert.NoError(t, New().StartTLS("127.0.0.1:8443", "cert.pem", "key.pem"))
}

type mockStarter struct {
	server *http.Server
	err    error
}

func (m *mockStarter) startTLSServer(srv *http.Server) error {
	m.server = srv
	return m.err
}

func TestStartAutoTLS_ConfiguresTheServer(t *testing.T) {
	starter := &mockStarter{}

	err := New().StartAutoTLSWithStarter("example.com", starter)

	assert.NoError(t, err)
	assert.Equal(t, ":443", starter.server.Addr)
	assert.NotNil(t, starter.server.TLSConfig.GetCertificate)
	assert.Equal(t, defaultTimeouts.Write, starter.server.WriteTimeout)
}

func TestStartAutoTLS_ReturnsTheError(t *testing.T) {
	starter := &mockStarter{err: errors.New("tls error")}

	assert.ErrorIs(t, New().StartAutoTLSWithStarter("example.com", starter), starter.err)
}
