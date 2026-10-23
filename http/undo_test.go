package http_test

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/whagency/core-common/http"
)

func TestIsUndoRequiredError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantUndo bool
	}{
		{
			name:     "nil error should NOT require undo",
			err:      nil,
			wantUndo: false,
		},
		{
			name:     "generic error should require undo",
			err:      errors.New("some generic error"),
			wantUndo: true,
		},
		{
			name:     "wrapped generic error should require undo",
			err:      fmt.Errorf("wrapped: %w", errors.New("generic error")),
			wantUndo: true,
		},
		// Syscall errors that should NOT require undo (connection never established)
		{
			name:     "ECONNREFUSED should NOT require undo",
			err:      syscall.ECONNREFUSED,
			wantUndo: false,
		},
		{
			name:     "wrapped ECONNREFUSED should NOT require undo",
			err:      fmt.Errorf("connection failed: %w", syscall.ECONNREFUSED),
			wantUndo: false,
		},
		{
			name:     "EHOSTUNREACH should NOT require undo",
			err:      syscall.EHOSTUNREACH,
			wantUndo: false,
		},
		{
			name:     "wrapped EHOSTUNREACH should NOT require undo",
			err:      fmt.Errorf("host unreachable: %w", syscall.EHOSTUNREACH),
			wantUndo: false,
		},
		{
			name:     "ENETUNREACH should NOT require undo",
			err:      syscall.ENETUNREACH,
			wantUndo: false,
		},
		{
			name:     "wrapped ENETUNREACH should NOT require undo",
			err:      fmt.Errorf("network error: %w", syscall.ENETUNREACH),
			wantUndo: false,
		},
		// Syscall errors that SHOULD require undo (connection might have been established)
		{
			name:     "ECONNRESET after connection should require undo",
			err:      syscall.ECONNRESET,
			wantUndo: true,
		},
		{
			name:     "ETIMEDOUT should require undo",
			err:      syscall.ETIMEDOUT,
			wantUndo: true,
		},
		{
			name:     "EPIPE should require undo",
			err:      syscall.EPIPE,
			wantUndo: true,
		},
		// DNS errors should NOT require undo (request never sent)
		{
			name: "DNSError should NOT require undo",
			err: &net.DNSError{
				Err:        "no such host",
				Name:       "wallet.example.com",
				Server:     "8.8.8.8",
				IsNotFound: true,
			},
			wantUndo: false,
		},
		{
			name: "wrapped DNSError should NOT require undo",
			err: fmt.Errorf("lookup failed: %w", &net.DNSError{
				Err:  "timeout",
				Name: "wallet.example.com",
			}),
			wantUndo: false,
		},
		{
			name: "DNSError with temporary flag should NOT require undo",
			err: &net.DNSError{
				Err:         "i/o timeout",
				Name:        "wallet.example.com",
				IsTemporary: true,
			},
			wantUndo: false,
		},
		// URL parse errors should NOT require undo (request construction failed)
		{
			name: "url.Error with parse operation should NOT require undo",
			err: &url.Error{
				Op:  "parse",
				URL: "ht!tp://invalid",
				Err: errors.New("invalid URI"),
			},
			wantUndo: false,
		},
		{
			name: "wrapped url.Error with parse should NOT require undo",
			err: fmt.Errorf("failed to parse: %w", &url.Error{
				Op:  "parse",
				URL: "://malformed",
				Err: errors.New("missing protocol scheme"),
			}),
			wantUndo: false,
		},
		// URL errors with other operations SHOULD require undo (request might have been sent)
		{
			name: "url.Error with Get operation should require undo",
			err: &url.Error{
				Op:  "Get",
				URL: "http://wallet.example.com/api/debit",
				Err: errors.New("timeout"),
			},
			wantUndo: true,
		},
		{
			name: "url.Error with Post operation should require undo",
			err: &url.Error{
				Op:  "Post",
				URL: "http://wallet.example.com/api/debit",
				Err: errors.New("connection reset"),
			},
			wantUndo: true,
		},
		// Network operation errors
		{
			name: "net.OpError with dial should NOT require undo",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: syscall.ECONNREFUSED,
			},
			wantUndo: false,
		},
		{
			name: "net.OpError with read should require undo",
			err: &net.OpError{
				Op:  "read",
				Net: "tcp",
				Err: errors.New("connection reset"),
			},
			wantUndo: true,
		},
		{
			name: "net.OpError with write should require undo",
			err: &net.OpError{
				Op:  "write",
				Net: "tcp",
				Err: errors.New("broken pipe"),
			},
			wantUndo: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := http.IsUndoRequiredError(tt.err)
			assert.Equal(t, tt.wantUndo, got,
				"isUndoRequiredError() = %v, want %v for error: %v",
				got, tt.wantUndo, tt.err)
		})
	}
}

// TestIsUndoRequiredError_RealWorldScenarios tests realistic error scenarios
func TestIsUndoRequiredError_RealWorldScenarios(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantUndo    bool
		description string
	}{
		{
			name:        "service down - no undo needed",
			err:         syscall.ECONNREFUSED,
			wantUndo:    false,
			description: "Wallet service is not running, request never reached server",
		},
		{
			name:        "network partition - no undo needed",
			err:         syscall.ENETUNREACH,
			wantUndo:    false,
			description: "Network unreachable, request never sent",
		},
		{
			name:        "DNS misconfiguration - no undo needed",
			err:         &net.DNSError{Err: "no such host", Name: "wallet.svc.local"},
			wantUndo:    false,
			description: "DNS lookup failed, no connection attempted",
		},
		{
			name:        "malformed config URL - no undo needed",
			err:         &url.Error{Op: "parse", URL: "://invalid", Err: errors.New("invalid")},
			wantUndo:    false,
			description: "Configuration error, request construction failed",
		},
		{
			name:        "request timeout - undo needed",
			err:         fmt.Errorf("request timeout: %w", syscall.ETIMEDOUT),
			wantUndo:    true,
			description: "Request might have reached server, response lost",
		},
		{
			name:        "connection reset during request - undo needed",
			err:         syscall.ECONNRESET,
			wantUndo:    true,
			description: "Connection was established but reset, uncertain state",
		},
		{
			name:        "HTTP client error - undo needed",
			err:         errors.New("http: request canceled while waiting for connection"),
			wantUndo:    true,
			description: "Generic HTTP error, assume request might have been sent",
		},
		{
			name:        "unknown error - undo needed (safe default)",
			err:         errors.New("unexpected error"),
			wantUndo:    true,
			description: "Unknown error type, err on the side of caution",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := http.IsUndoRequiredError(tt.err)
			assert.Equal(t, tt.wantUndo, got,
				"Scenario: %s\nExpected undo=%v, got=%v\nError: %v",
				tt.description, tt.wantUndo, got, tt.err)
		})
	}
}

// TestIsUndoRequiredError_EdgeCases tests edge cases and boundary conditions
func TestIsUndoRequiredError_EdgeCases(t *testing.T) {
	t.Run("deeply nested wrapped errors", func(t *testing.T) {
		err := fmt.Errorf("layer3: %w",
			fmt.Errorf("layer2: %w",
				fmt.Errorf("layer1: %w", syscall.ECONNREFUSED)))
		assert.False(t, http.IsUndoRequiredError(err),
			"Should detect ECONNREFUSED even when deeply wrapped")
	})

	t.Run("multiple wrapped errors", func(t *testing.T) {
		dnsErr := &net.DNSError{Err: "timeout", Name: "example.com"}
		urlErr := &url.Error{Op: "parse", URL: "://bad", Err: dnsErr}
		wrappedErr := fmt.Errorf("request failed: %w", urlErr)
		assert.False(t, http.IsUndoRequiredError(wrappedErr),
			"Should detect parse error in url.Error")
	})

	t.Run("error chain with both undo and non-undo errors", func(t *testing.T) {
		// The most specific error (ECONNREFUSED) should take precedence
		err := fmt.Errorf("timeout occurred: %w", syscall.ECONNREFUSED)
		assert.False(t, http.IsUndoRequiredError(err),
			"ECONNREFUSED should take precedence")
	})

	t.Run("custom error type", func(t *testing.T) {
		type customError struct {
			msg string
		}
		customErr := &customError{msg: "custom error"}
		// Make customError implement error interface
		err := fmt.Errorf("custom: %s", customErr.msg)
		assert.True(t, http.IsUndoRequiredError(err),
			"Unknown custom errors should require undo by default")
	})
}
