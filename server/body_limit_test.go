// Copyright 2026 Google LLC
// SPDX-License-Identifier: MIT

package server

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newBodyLimitTestContext(limit int64) *Context {
	cfg := defaultSettings
	cfg.BodyLimit = limit

	return NewContext(&cfg)
}

func TestContextBodyLimit_DirectRead(t *testing.T) {
	const limit = 10

	tests := []struct {
		name        string
		body        string
		wantErr     bool
		wantReadLen int
	}{
		{
			name:        "body smaller than limit",
			body:        "hello",
			wantErr:     false,
			wantReadLen: 5,
		},
		{
			name:        "body exactly at limit",
			body:        "0123456789",
			wantErr:     false,
			wantReadLen: 10,
		},
		{
			name:        "body exceeds limit",
			body:        "0123456789EXTRA",
			wantErr:     true,
			wantReadLen: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newBodyLimitTestContext(limit)

			req := httptest.NewRequest(
				http.MethodPost,
				"/",
				strings.NewReader(tt.body),
			)

			c.Reset(httptest.NewRecorder(), req)

			require.True(t, c.bodyLimited)

			// Simulate a handler reading the body directly,
			// without using Falcon's binding methods.
			got, err := io.ReadAll(c.Request.Body)

			assert.Len(t, got, tt.wantReadLen)

			if tt.wantErr {
				require.Error(t, err)

				var maxBytesErr *http.MaxBytesError
				require.True(t, errors.As(err, &maxBytesErr))
				assert.EqualValues(t, limit, maxBytesErr.Limit)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestContextBodyLimit_ResetsOnContextReuse(t *testing.T) {
	const limit = 8

	c := newBodyLimitTestContext(limit)

	firstReq := httptest.NewRequest(
		http.MethodPost,
		"/first",
		strings.NewReader("123456789"),
	)

	c.Reset(httptest.NewRecorder(), firstReq)

	require.True(t, c.bodyLimited)

	_, err := io.ReadAll(c.Request.Body)

	var maxBytesErr *http.MaxBytesError
	require.ErrorAs(t, err, &maxBytesErr)

	// Reuse the same Context for a new request.
	secondReq := httptest.NewRequest(
		http.MethodPost,
		"/second",
		strings.NewReader("abcdefghijk"),
	)

	c.Reset(httptest.NewRecorder(), secondReq)

	require.True(t, c.bodyLimited)

	got, err := io.ReadAll(c.Request.Body)

	assert.Len(t, got, int(limit))
	require.ErrorAs(t, err, &maxBytesErr)
	assert.EqualValues(t, limit, maxBytesErr.Limit)
}

func TestContextBodyLimit_NilBody(t *testing.T) {
	c := newBodyLimitTestContext(10)

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	// Explicitly test a nil request body.
	req.Body = nil

	assert.NotPanics(t, func() {
		c.Reset(httptest.NewRecorder(), req)
	})

	assert.False(t, c.bodyLimited)
	assert.Nil(t, c.Request.Body)
}

func TestContextBodyLimit_NilRequest(t *testing.T) {
	c := newBodyLimitTestContext(10)

	assert.NotPanics(t, func() {
		c.Reset(httptest.NewRecorder(), nil)
	})

	assert.Nil(t, c.Request)
	assert.False(t, c.bodyLimited)
}

func TestContextBodyLimit_EnforcedAfterPartialRead(t *testing.T) {
	const limit = 10

	c := newBodyLimitTestContext(limit)

	req := httptest.NewRequest(
		http.MethodPost,
		"/",
		strings.NewReader("0123456789EXTRA"),
	)

	c.Reset(httptest.NewRecorder(), req)

	first := make([]byte, 5)
	n, err := c.Request.Body.Read(first)

	require.NoError(t, err)
	assert.Equal(t, 5, n)

	remaining, err := io.ReadAll(c.Request.Body)

	assert.Len(t, remaining, 5)

	var maxBytesErr *http.MaxBytesError
	require.ErrorAs(t, err, &maxBytesErr)
	assert.EqualValues(t, limit, maxBytesErr.Limit)
}
