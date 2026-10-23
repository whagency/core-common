package tracer

import (
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/whagency/core-common/appenv"
	"github.com/whagency/core-common/tools"
)

var config Config // holds the configuration for the tracer

type Config struct {
	AppName     string
	Environment string
	Version     string

	Enable bool `envconfig:"TRACER_ENABLE" default:"false"` // enable tracer

	// "http" or "grpc" (for Jaeger all-in-one + your request: "http")
	Protocol string `envconfig:"TRACER_PROTOCOL" default:"http"`

	// host:port (http: jaeger:4318)
	Endpoint string `envconfig:"TRACER_ENDPOINT"`

	// only for Protocol="http" (default: "/v1/traces")
	HTTPPath string `envconfig:"TRACER_HTTP_PATH" default:"/v1/traces"`

	// 0..1 (default 1.0). ParentBased sampling.
	SampleRatio float64 `envconfig:"TRACER_SAMPLE_RATIO" default:"1.0"`

	// Optional: max time spans wait in the queue before being flushed.
	// Zero/unset keeps the OTel SDK default (5s).
	BatchTimeout time.Duration `envconfig:"TRACER_BATCH_TIMEOUT"`

	// Optional: max spans per export batch.
	// Zero/unset keeps the OTel SDK default (512).
	MaxExportBatchSize int `envconfig:"TRACER_MAX_EXPORT_BATCH_SIZE"`

	// TLS (optional)
	TLSEnabled bool `envconfig:"TRACER_TLS_ENABLE" default:"false"`

	// Optional CA certificate path (PEM). Needed for private/self-signed CA.
	CAPath string `envconfig:"TRACER_CA_PATH"`

	// mTLS (optional). If used, both must be provided.
	CertPath string `envconfig:"TRACER_CERT_PATH"`
	KeyPath  string `envconfig:"TRACER_KEY_PATH"`

	// Optional SNI override. If empty, host from Endpoint will be used.
	ServerName string `envconfig:"TRACER_SERVER_NAME"`

	// Optional: skip TLS verification (useful for dev). Avoid in production.
	TLSInsecureSkipVerify bool `envconfig:"TRACER_TLS_INSECURE_SKIP_VERIFY" default:"false"`

	HTTPNotTraceablePaths string `envconfig:"TRACER_HTTP_NOT_TRACEABLE_PATHS"` // comma-separated paths to skip tracing

	// Internal use only
	httpNotTraceablePaths []string // parsed http paths to skip tracing
}

// GetConfig returns the current tracer configuration.
func GetConfig() Config {
	return config
}

// GetHTTPNotTraceablePaths returns the list of http paths that should not be traced.
func (c Config) GetHTTPNotTraceablePaths() []string {
	return c.httpNotTraceablePaths
}

// validateAndSetConfig validates the configuration and sets default values if needed.
func validateAndSetConfig(cfg Config) error {
	if err := appenv.ValidateAppName(cfg.AppName); err != nil {
		return fmt.Errorf("[validateAndSetConfig][err: %w]", err)
	}

	normalizedEnvironment, err := appenv.Normalize(cfg.Environment)
	if err != nil {
		return fmt.Errorf("[validateAndSetConfig][err: %w]", err)
	}
	cfg.Environment = normalizedEnvironment

	if cfg.Enable {
		if strings.TrimSpace(cfg.Endpoint) == "" {
			return errors.New("[validateAndSetConfig][err: endpoint is required when tracer is enabled]")
		}
	}

	// mTLS requires both CertPath and KeyPath, or neither (CAPath is optional)
	certProvided := strings.TrimSpace(cfg.CertPath) != ""
	keyProvided := strings.TrimSpace(cfg.KeyPath) != ""
	if certProvided != keyProvided {
		return errors.New("[validateAndSetConfig][err: both certPath and keyPath are required for mTLS]")
	}

	if strings.TrimSpace(cfg.Version) == "" {
		cfg.Version = "unknown"
	}

	if strings.ToLower(strings.TrimSpace(cfg.Protocol)) == "" {
		cfg.Protocol = "http"
	}

	if cfg.Protocol == "http" {
		if strings.TrimSpace(cfg.HTTPPath) == "" {
			cfg.HTTPPath = "/v1/traces"
		}
		if !strings.HasPrefix(cfg.HTTPPath, "/") {
			cfg.HTTPPath = "/" + cfg.HTTPPath
		}
	}

	if cfg.SampleRatio < 0 {
		cfg.SampleRatio = 0
	}

	if cfg.SampleRatio > 1 {
		cfg.SampleRatio = 1
	}

	// Negative values are invalid input, not "unset" — normalize to unset so the
	// OTel SDK default is used instead of silently passing a bogus value.
	if cfg.BatchTimeout < 0 {
		cfg.BatchTimeout = 0
	}

	if cfg.MaxExportBatchSize < 0 {
		cfg.MaxExportBatchSize = 0
	}

	if cfg.HTTPNotTraceablePaths != "" {
		cfg.httpNotTraceablePaths = strings.Split(cfg.HTTPNotTraceablePaths, ",")
		for i := range cfg.httpNotTraceablePaths {
			cfg.httpNotTraceablePaths[i] = strings.TrimSpace(cfg.httpNotTraceablePaths[i])
		}
	}

	// set cfg globally
	config = cfg

	return nil
}

// buildTLSConfig builds a tls.Config based on the provided Config.
func buildTLSConfig(cfg Config) (*tls.Config, error) {
	// Optional early validation of mTLS pair (so error messages are tracer-specific).
	if (strings.TrimSpace(cfg.CertPath) == "") != (strings.TrimSpace(cfg.KeyPath) == "") {
		return nil, fmt.Errorf("[tracer][buildTLSConfig][err: mTLS requires both TRACER_CERT_PATH and TRACER_KEY_PATH]")
	}

	tlsCfg, err := tools.BuildTLSConfig(tools.TLSOptions{
		Endpoint:           cfg.Endpoint,
		ServerName:         cfg.ServerName,
		CAPath:             cfg.CAPath,
		CertPath:           cfg.CertPath,
		KeyPath:            cfg.KeyPath,
		InsecureSkipVerify: cfg.TLSInsecureSkipVerify,
		MinVersion:         tls.VersionTLS12,
	})
	if err != nil {
		return nil, fmt.Errorf("[tracer][buildTLSConfig]%w", err)
	}
	return tlsCfg, nil
}
