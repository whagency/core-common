package tools

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strings"
)

type TLSOptions struct {
	Endpoint   string // host:port or host (used to derive default SNI)
	ServerName string // optional SNI override

	CAPath   string // optional CA PEM (needed for private/self-signed CA)
	CertPath string // optional client cert PEM (mTLS)
	KeyPath  string // optional client key PEM (mTLS)

	InsecureSkipVerify bool // DO NOT use in production unless you understand the risks

	MinVersion uint16 // optional; if 0 -> tls.VersionTLS12
}

// BuildTLSConfig constructs a TLS configuration based on provided options.
func BuildTLSConfig(opts TLSOptions) (*tls.Config, error) {
	endpoint := strings.TrimSpace(opts.Endpoint)

	host := endpoint
	if endpoint != "" {
		if h, _, err := net.SplitHostPort(endpoint); err == nil && h != "" {
			host = h
		}
	}

	serverName := strings.TrimSpace(opts.ServerName)
	if serverName == "" {
		serverName = host
	}

	minVersion := opts.MinVersion
	if minVersion == 0 {
		minVersion = tls.VersionTLS12
	}

	tlsCfg := &tls.Config{
		MinVersion:         minVersion,
		ServerName:         serverName,
		InsecureSkipVerify: opts.InsecureSkipVerify,
	}

	// Optional custom CA
	if strings.TrimSpace(opts.CAPath) != "" {
		caCert, err := os.ReadFile(opts.CAPath)
		if err != nil {
			return nil, fmt.Errorf("[tools][BuildTLSConfig][err: read CA certificate: %w]", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("[tools][BuildTLSConfig][err: append CA certificate to pool]")
		}
		tlsCfg.RootCAs = pool
	}

	// Optional mTLS
	certPath := strings.TrimSpace(opts.CertPath)
	keyPath := strings.TrimSpace(opts.KeyPath)
	if certPath != "" && keyPath != "" {
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, fmt.Errorf("[tools][BuildTLSConfig][err: load client certificate/key: %w]", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return tlsCfg, nil
}
