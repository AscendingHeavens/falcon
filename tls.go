// Copyright 2026 Google LLC
// SPDX-License-Identifier: MIT

package falcon

import (
	"log"
	"net"
	"net/http"

	"golang.org/x/crypto/acme/autocert"
)

// listenAndServeTLS allows TLS server startup to be mocked during testing.
var listenAndServeTLS = func(srv *http.Server, certFile, keyFile string) error {
	return srv.ListenAndServeTLS(certFile, keyFile)
}

// listenTCP allows TCP listener creation to be mocked during testing.
var listenTCP = net.Listen

// StartTLS starts the server with TLS using the provided certificate and key files.
//
// It blocks until the server stops, returning nil after Shutdown and otherwise
// the error that stopped it.
func (s *Server) StartTLS(addr, certFile, keyFile string) error {
	log.Printf("Starting server with TLS on %s", addr)

	return ignoreClosed(
		listenAndServeTLS(s.newHTTPServer(addr), certFile, keyFile),
	)
}

// startTLSServer starts an HTTP server with TLS using the provided configuration.
// This method implements the TLSStarter interface.
func (s *Server) startTLSServer(server *http.Server) error {
	return server.ListenAndServeTLS("", "")
}

// StartAutoTLS starts the server with automatic TLS certificate management
// using Let's Encrypt.
//
// The HTTPS server listens on port 443. An HTTP listener on port 80 is also
// started to handle ACME HTTP-01 challenges.
//
// Both listeners are managed by this method. When the HTTPS server stops,
// the HTTP listener is closed as well.
//
// Requirements:
//   - The domain must point to the server's public IP address.
//   - Ports 80 and 443 must be accessible from the internet.
//   - The application must have permission to bind to both ports.
func (s *Server) StartAutoTLS(domain string) error {
	return s.StartAutoTLSWithStarter(domain, s)
}

// StartAutoTLSWithStarter starts the server with automatic TLS certificate
// management using a custom TLSStarter.
//
// The custom starter allows TLS startup to be mocked during testing.
// For production use, prefer StartAutoTLS.
//
// The method starts an HTTP listener on port 80 for ACME HTTP-01 challenges
// and an HTTPS listener on port 443 for application traffic and TLS-ALPN-01
// challenges.
//
// The HTTP listener is closed when the HTTPS starter returns.
func (s *Server) StartAutoTLSWithStarter(
	domain string,
	starter TLSStarter,
) error {
	manager := &autocert.Manager{
		Cache:      autocert.DirCache("certs"),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(domain),
	}

	server := s.newHTTPServer(":443")
	server.TLSConfig = manager.TLSConfig()

	// Bind port 80 synchronously so that startup failures are returned.
	listener, err := listenTCP("tcp", ":80")
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Handler: manager.HTTPHandler(nil),
	}

	// Start the HTTP listener for ACME HTTP-01 challenges.
	httpDone := make(chan struct{})

	go func() {
		defer close(httpDone)

		if err := httpServer.Serve(listener); err != nil &&
			err != http.ErrServerClosed {
			log.Printf("AutoTLS HTTP listener error: %v", err)
		}
	}()

	log.Printf("Starting server with AutoTLS on %s", domain)

	// Always stop the HTTP listener when the HTTPS server returns.
	defer func() {
		if err := httpServer.Close(); err != nil &&
			err != http.ErrServerClosed {
			log.Printf("AutoTLS HTTP listener shutdown error: %v", err)
		}

		// Wait until the HTTP server has completely stopped.
		<-httpDone
	}()

	return ignoreClosed(starter.startTLSServer(server))
}
