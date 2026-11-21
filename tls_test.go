// Copyright 2026 Google LLC
// SPDX-License-Identifier: MIT

package falcon

import (
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockListenAndServeTLS replaces the real TLS startup for testing.
func mockListenAndServeTLS(
	t *testing.T,
	mock func(*http.Server, string, string) error,
) {
	t.Helper()

	original := listenAndServeTLS
	listenAndServeTLS = mock

	t.Cleanup(func() {
		listenAndServeTLS = original
	})
}

// mockListenTCP replaces the real TCP listener with an ephemeral listener.
// This prevents tests from requiring access to port 80.
func mockListenTCP(t *testing.T) *net.Listener {
	t.Helper()

	original := listenTCP

	var listener net.Listener

	listenTCP = func(network, address string) (net.Listener, error) {
		var err error

		listener, err = net.Listen("tcp", "127.0.0.1:0")
		return listener, err
	}

	t.Cleanup(func() {
		listenTCP = original

		if listener != nil {
			_ = listener.Close()
		}
	})

	return &listener
}

func TestStartTLS_CallsListenAndServeTLS(t *testing.T) {
	var gotServer *http.Server
	var cert, key string

	mockListenAndServeTLS(t, func(
		srv *http.Server,
		c, k string,
	) error {
		gotServer, cert, key = srv, c, k
		return nil
	})

	srv := New()
	err := srv.StartTLS("127.0.0.1:8443", "cert.pem", "key.pem")

	assert.NoError(t, err)
	require.NotNil(t, gotServer)

	assert.Equal(t, "127.0.0.1:8443", gotServer.Addr)
	assert.Equal(t, srv, gotServer.Handler)
	assert.Equal(t, "cert.pem", cert)
	assert.Equal(t, "key.pem", key)
	assert.Equal(t, defaultTimeouts.ReadHeader, gotServer.ReadHeaderTimeout)
}

func TestStartTLS_ReturnsTheError(t *testing.T) {
	tlsErr := errors.New("tls error")

	mockListenAndServeTLS(t, func(
		*http.Server,
		string,
		string,
	) error {
		return tlsErr
	})

	assert.ErrorIs(
		t,
		New().StartTLS("127.0.0.1:8443", "cert.pem", "key.pem"),
		tlsErr,
	)
}

func TestStartTLS_ShutdownIsNotAnError(t *testing.T) {
	mockListenAndServeTLS(t, func(
		*http.Server,
		string,
		string,
	) error {
		return http.ErrServerClosed
	})

	assert.NoError(
		t,
		New().StartTLS("127.0.0.1:8443", "cert.pem", "key.pem"),
	)
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
	mockListenTCP(t)

	starter := &mockStarter{}

	err := New().StartAutoTLSWithStarter("example.com", starter)

	require.NoError(t, err)
	require.NotNil(t, starter.server)
	require.NotNil(t, starter.server.TLSConfig)

	assert.Equal(t, ":443", starter.server.Addr)

	// The autocert manager must provide automatic certificate retrieval.
	assert.NotNil(t, starter.server.TLSConfig.GetCertificate)

	// TLS-ALPN-01 challenge support must be enabled.
	assert.Contains(
		t,
		starter.server.TLSConfig.NextProtos,
		"acme-tls/1",
	)

	assert.Equal(
		t,
		defaultTimeouts.Write,
		starter.server.WriteTimeout,
	)
}

func TestStartAutoTLS_ReturnsTheError(t *testing.T) {
	mockListenTCP(t)

	tlsErr := errors.New("tls error")
	starter := &mockStarter{err: tlsErr}

	assert.ErrorIs(
		t,
		New().StartAutoTLSWithStarter("example.com", starter),
		tlsErr,
	)
}

func TestStartAutoTLS_HTTPListenerStartsAndStops(t *testing.T) {
	listenerRef := mockListenTCP(t)

	started := make(chan struct{})
	stop := make(chan struct{})

	starter := &mockBlockingStarter{
		started: started,
		stop:    stop,
	}

	done := make(chan error, 1)

	go func() {
		done <- New().StartAutoTLSWithStarter("example.com", starter)
	}()

	// Wait until the HTTPS starter is invoked.
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("TLS starter was not called")
	}

	require.NotNil(t, *listenerRef)

	addr := (*listenerRef).Addr().String()

	// The HTTP listener should be active while HTTPS is running.
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	require.NoError(t, err)
	_ = conn.Close()

	// Allow the HTTPS starter to return.
	close(stop)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("AutoTLS did not stop")
	}

	// The HTTP listener must be closed after HTTPS stops.
	conn, err = net.DialTimeout("tcp", addr, 200*time.Millisecond)

	if conn != nil {
		_ = conn.Close()
	}

	assert.Error(t, err)
}

func TestStartAutoTLS_ReturnsHTTPListenerError(t *testing.T) {
	// Occupy an ephemeral port so that the injected listener fails to bind.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() {
		if err := occupied.Close(); err != nil {
			t.Errorf("failed to close occupied listener: %v", err)
		}
	}()

	original := listenTCP
	listenTCP = func(network, address string) (net.Listener, error) {
		return net.Listen("tcp", occupied.Addr().String())
	}
	t.Cleanup(func() {
		listenTCP = original
	})

	starter := &mockStarter{}

	err = New().StartAutoTLSWithStarter("example.com", starter)

	require.Error(t, err)
	assert.Nil(t, starter.server)
}

type mockBlockingStarter struct {
	started chan struct{}
	stop    chan struct{}
}

func (m *mockBlockingStarter) startTLSServer(
	srv *http.Server,
) error {
	close(m.started)
	<-m.stop

	return nil
}
