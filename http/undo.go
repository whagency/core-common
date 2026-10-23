package http

import (
	"errors"
	"net"
	"net/url"
	"syscall"
)

// IsUndoRequiredError determines whether an error requires an undo operation.
func IsUndoRequiredError(err error) bool {
	// If there is no error, no undo is required
	if err == nil {
		return false
	}

	// Check for syscall errors
	if errors.Is(err, syscall.ECONNREFUSED) { // connection refused by service, service process is not listening or running actively
		return false
	}

	if errors.Is(err, syscall.EHOSTUNREACH) { // host unreachable (network/routing issue)
		return false
	}

	if errors.Is(err, syscall.ENETUNREACH) { // network unreachable
		return false
	}

	// DNS resolution failures - request never sent
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return false
	}

	// Invalid URL/scheme - request construction failed
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Op == "parse" {
		return false
	}

	return true
}
