package logger

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	_defaultLogstashTCPConnectTimeout        = 5 * time.Second
	_defaultLogstashWriteTimeout             = 200 * time.Millisecond
	_defaultLogstashMaxRetries               = 5
	_defaultLogstashRetryInterval            = 100 * time.Millisecond
	_defaultLogstashReconnectorCheckInterval = 1 * time.Hour
)

// LogstashWriter writes to a TLS-secured Logstash server.
type LogstashWriter struct {
	conn net.Conn

	host           string
	tlsEnabled     bool
	tlsConfig      *tls.Config
	connectTimeout time.Duration
	writeTimeout   time.Duration
	maxRetries     int
	retryInterval  time.Duration

	reconnectorInterval time.Duration
	stopReconnector     chan struct{}

	mu      sync.Mutex
	enabled atomic.Bool
}

// NewLogstashWriter creates a new LogstashWriter with the provided configuration.
func NewLogstashWriter(cfg Config) (io.WriteCloser, error) {
	var tlsCfg *tls.Config
	var err error

	if cfg.LogstashTLSEnabled {
		// Build and configure TLS settings once
		tlsCfg, err = buildTLSConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("[logger:NewLogstashWriter]%w", err)
		}
	}

	w := &LogstashWriter{
		host:           cfg.LogstashHost,
		tlsConfig:      tlsCfg,
		tlsEnabled:     cfg.LogstashTLSEnabled,
		connectTimeout: cfg.LogstashTCPConnectTimeout,
		writeTimeout:   cfg.LogstashWriteTimeout,
		maxRetries:     cfg.LogstashMaxRetries,
		retryInterval:  cfg.LogstashRetryInterval,
	}

	// Set default values if not provided
	if w.connectTimeout <= 0 {
		w.connectTimeout = _defaultLogstashTCPConnectTimeout
	}
	if w.writeTimeout <= 0 {
		w.writeTimeout = _defaultLogstashWriteTimeout
	}
	if w.maxRetries <= 0 {
		w.maxRetries = _defaultLogstashMaxRetries
	}
	if w.retryInterval <= 0 {
		w.retryInterval = _defaultLogstashRetryInterval
	}

	// Initial connect
	if err = w.connect(); err != nil {
		return nil, fmt.Errorf("[logger:NewLogstashWriter]%w", err)
	}

	// Start the reconnector if enabled
	if cfg.LogstashReconnectorEnabled {
		w.reconnectorInterval = cfg.LogstashReconnectorCheckInterval
		if w.reconnectorInterval <= 0 {
			w.reconnectorInterval = _defaultLogstashReconnectorCheckInterval
		}
		w.startReconnector()
	}

	return w, nil
}

// StartReconnector starts a goroutine that attempts to reconnect to Logstash periodically.
func (w *LogstashWriter) startReconnector() {
	w.stopReconnector = make(chan struct{})
	ticker := time.NewTicker(w.reconnectorInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := w.reconnect(); err != nil {
					fmt.Printf("[LogstashWriter:startReconnector]%s", err)
				}
			case <-w.stopReconnector:
				return
			}
		}
	}()
}

// reconnect attempts to reconnect to the Logstash server if not already connected.
func (w *LogstashWriter) reconnect() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.enabled.Load() {
		return nil
	}

	if err := w.connect(); err != nil {
		return fmt.Errorf("[LogstashWriter:reconnect][err: %w]", err)
	}

	return nil
}

// connect establishes a TLS connection to the Logstash server.
func (w *LogstashWriter) connect() error {
	if w.enabled.Load() {
		return nil // Already connected
	}

	// Create a new dialer with a timeout
	if w.tlsEnabled {
		// Create a new dialer with a timeout
		dialer := &net.Dialer{Timeout: w.connectTimeout}
		tlsConn, err := tls.DialWithDialer(dialer, "tcp", w.host, w.tlsConfig)
		if err != nil {
			return fmt.Errorf("[connect][tls]%w", err)
		}
		w.conn = tlsConn
		w.enabled.Store(true) // Set status to online after successful connection
		return nil
	}

	plainConn, err := net.DialTimeout("tcp", w.host, w.connectTimeout)
	if err != nil {
		return fmt.Errorf("[connect][tcp]%w", err)
	}
	w.conn = plainConn
	w.enabled.Store(true)

	return nil
}

// Write writes p to Logstash with a write deadline.
func (w *LogstashWriter) Write(p []byte) (n int, err error) {
	// Fast path: if disabled, skip locking to reduce hot-path overhead.
	if !w.enabled.Load() {
		return len(p), nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.enabled.Load() {
		return len(p), nil
	}

	for i := 0; i < w.maxRetries; i++ {
		if err = w.connect(); err != nil {
			w.exponentialBackoff(i)
			continue
		}

		if err = w.conn.SetWriteDeadline(time.Now().Add(w.writeTimeout)); err != nil {
			w.exponentialBackoff(i)
			err = fmt.Errorf("[err: failed to set write deadline: %w]", err)
			continue
		}

		n, err = w.conn.Write(p)
		if err == nil {
			return n, err
		}

		// Retry on error
		w.exponentialBackoff(i)
	}

	return 0, fmt.Errorf("[LogstashWriter:Write][reached max retries while writing to Logstash server: %s]%w", w.host, err)
}

// exponentialBackoff implements an exponential backoff strategy for retries.
func (w *LogstashWriter) exponentialBackoff(attempt int) {
	if err := w.disable(); err != nil {
		fmt.Printf("[LogstashWriter:exponentialBackoff]%s\n", err)
	}
	if attempt < (w.maxRetries - 1) {
		time.Sleep(w.retryInterval * time.Duration(1<<attempt))
	}
}

// disable disables the LogstashWriter, closing the connection if it exists.
func (w *LogstashWriter) disable() (err error) {
	w.enabled.Store(false)
	if w.conn != nil {
		if errClose := w.conn.Close(); errClose != nil {
			err = fmt.Errorf("[disable][err: %w]", errClose)
		}
		w.conn = nil
	}

	return err
}

// Close closes the connection to the Logstash server.
func (w *LogstashWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopReconnector != nil {
		close(w.stopReconnector)
		w.stopReconnector = nil
	}

	if err := w.disable(); err != nil {
		return fmt.Errorf("[LogstashWriter:Close]%w", err)
	}

	return nil
}
