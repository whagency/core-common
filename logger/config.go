package logger

import (
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/whagency/core-common/appenv"
	"github.com/whagency/core-common/tools"
	"go.uber.org/zap"
)

var config Config // holds the configuration for the logger

const (
	StdoutFormatConsole = "console"
	StdoutFormatJSON    = "json"
)

type Config struct {
	AppName     string // application name
	Environment string // app environment (local, dev, stage, production, test)
	Version     string // application version

	Level string `envconfig:"LOG_LEVEL" default:"debug"` // log level ("debug", "info", etc.)

	StdoutFormat string `envconfig:"LOG_STDOUT_FORMAT" default:"console"` // stdout log format: "console" or "json"

	CAPath   string `envconfig:"LOG_CA_PATH"`   // optional CA certificate path (PEM). Needed for private/self-signed CA.
	CertPath string `envconfig:"LOG_CERT_PATH"` // optional client certificate path (PEM) for mTLS
	KeyPath  string `envconfig:"LOG_KEY_PATH"`  // optional client key path (PEM) for mTLS

	LogstashEnabled                  bool          `envconfig:"LOG_LOGSTASH_ENABLE" default:"false"`
	LogstashTLSEnabled               bool          `envconfig:"LOG_LOGSTASH_TLS_ENABLE" default:"false"`
	LogstashTLSServerName            string        `envconfig:"LOG_LOGSTASH_TLS_SERVER_NAME"`
	LogstashTLSInsecureSkipVerify    bool          `envconfig:"LOG_LOGSTASH_TLS_INSECURE_SKIP_VERIFY" default:"false"`
	LogstashHost                     string        `envconfig:"LOG_LOGSTASH_HOST"`                                    // host:port for Logstash
	LogstashTCPConnectTimeout        time.Duration `envconfig:"LOG_LOGSTASH_TCP_CONNECT_TIMEOUT" default:"5s"`        // timeout for connecting to Logstash over TCP
	LogstashWriteTimeout             time.Duration `envconfig:"LOG_LOGSTASH_WRITE_TIMEOUT" default:"200ms"`           // timeout for writing to Logstash
	LogstashMaxRetries               int           `envconfig:"LOG_LOGSTASH_MAX_RETRIES" default:"5"`                 // max retries for Logstash connection
	LogstashRetryInterval            time.Duration `envconfig:"LOG_LOGSTASH_RETRY_INTERVAL" default:"10ms"`           // interval between retries for Logstash
	LogstashReconnectorEnabled       bool          `envconfig:"LOG_LOGSTASH_RECONNECTOR_ENABLED" default:"true"`      // enable automatic reconnection to Logstash
	LogstashReconnectorCheckInterval time.Duration `envconfig:"LOG_LOGSTASH_RECONNECTOR_CHECK_INTERVAL" default:"1h"` // interval to check Logstash connection status

	LogBodyWithInfoLevel bool   `envconfig:"LOG_BODY_WITH_INFO_LEVEL"`    // whether to log request/response body at info level
	HTTPNotLoggablePaths string `envconfig:"LOG_HTTP_NOT_LOGGABLE_PATHS"` // comma-separated paths to skip logging

	// Internal use only
	httpNotLoggablePaths []string // parsed http paths to skip logging
}

// GetConfig returns the current logger configuration.
func GetConfig() Config {
	return config
}

// GetHTTPNotLoggablePaths returns the list of http paths that should not be logged.
func (c Config) GetHTTPNotLoggablePaths() []string {
	return c.httpNotLoggablePaths
}

// IsLoggableBody returns true if the request/response body should be logged.
func IsLoggableBody() bool {
	return config.Level == zap.DebugLevel.String() || (config.Level == zap.InfoLevel.String() && config.LogBodyWithInfoLevel)
}

// validateAndSetConfig validates the configuration and sets default values if needed.
func validateAndSetConfig(cfg Config) error {
	if err := appenv.ValidateAppName(cfg.AppName); err != nil {
		return fmt.Errorf("[logger:validateConfig][err: %w]", err)
	}

	normalizedEnvironment, err := appenv.Normalize(cfg.Environment)
	if err != nil {
		return fmt.Errorf("[logger:validateConfig][err: %w]", err)
	}
	cfg.Environment = normalizedEnvironment

	if cfg.Level == "" {
		return fmt.Errorf("[logger:validateConfig][err: log level must be set]")
	}

	if cfg.Version == "" {
		cfg.Version = "unknown"
	}

	stdoutFormat, err := normalizeStdoutFormat(cfg.StdoutFormat)
	if err != nil {
		return err
	}
	cfg.StdoutFormat = stdoutFormat

	if cfg.LogstashEnabled {
		if cfg.LogstashHost == "" {
			return fmt.Errorf("[logger:validateConfig][err: Logstash enabled but missing host]")
		}
		if cfg.LogstashTLSEnabled {
			// CAPath is optional (needed only for private/self-signed CA).
			// mTLS is optional, but if used, both cert and key must be provided.
			if (strings.TrimSpace(cfg.CertPath) == "") != (strings.TrimSpace(cfg.KeyPath) == "") {
				return fmt.Errorf("[logger:validateConfig][err: Logstash mTLS requires both LOG_CERT_PATCH and LOG_KEY_PATCH]")
			}
		}
	}

	// parse not loggable http paths
	if cfg.HTTPNotLoggablePaths != "" {
		cfg.httpNotLoggablePaths = strings.Split(cfg.HTTPNotLoggablePaths, ",")
		for i := range cfg.httpNotLoggablePaths {
			cfg.httpNotLoggablePaths[i] = strings.TrimSpace(cfg.httpNotLoggablePaths[i])
		}
	}

	// set cfg globally
	config = cfg

	return nil
}

// normalizeStdoutFormat validates and normalizes the stdout log format, returning an error if the format is unsupported.
func normalizeStdoutFormat(stdoutFormat string) (string, error) {
	format := strings.ToLower(strings.TrimSpace(stdoutFormat))
	if format == "" {
		format = StdoutFormatConsole
	}

	switch format {
	case StdoutFormatConsole, StdoutFormatJSON:
		return format, nil
	default:
		return "", fmt.Errorf("[logger:validateConfig][err: unsupported LOG_STDOUT_FORMAT %q (allowed: %q, %q)]",
			stdoutFormat,
			StdoutFormatConsole,
			StdoutFormatJSON,
		)
	}
}

// buildTLSConfig loads certificates and constructs a tls.Config.
func buildTLSConfig(cfg Config) (*tls.Config, error) {
	tlsCfg, err := tools.BuildTLSConfig(tools.TLSOptions{
		Endpoint:           cfg.LogstashHost, // SNI from host part
		ServerName:         cfg.LogstashTLSServerName,
		CAPath:             cfg.CAPath,
		CertPath:           cfg.CertPath,
		KeyPath:            cfg.KeyPath,
		InsecureSkipVerify: cfg.LogstashTLSInsecureSkipVerify,
		MinVersion:         tls.VersionTLS12,
	})
	if err != nil {
		return nil, fmt.Errorf("[logger][buildTLSConfig]%w", err)
	}
	return tlsCfg, nil
}
