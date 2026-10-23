package http

import (
	"time"

	"github.com/imroc/req/v3"
	"github.com/whagency/core-common/services"
)

type Options struct {
	timeoutExtCall      time.Duration
	securityTokenHeader string
}

type Option func(*Options)

type Service struct {
	client                *req.Client
	restrictionServiceURL string
	securityToken         string
	timeoutExtCall        time.Duration
	securityTokenHeader   string
}

// WithTimeoutExtCall sets the timeout for external calls to the restriction service.
func WithTimeoutExtCall(timeout time.Duration) Option {
	return func(o *Options) {
		o.timeoutExtCall = timeout
	}
}

// WithSecurityTokenHeader sets the header name for the security token.
func WithSecurityTokenHeader(header string) Option {
	return func(o *Options) {
		o.securityTokenHeader = header
	}
}

// defaultServiceOptions provides the default options for the restriction service.
func defaultServiceOptions() *Options {
	return &Options{
		timeoutExtCall:      services.DefaultTimeoutExtCall,
		securityTokenHeader: services.DefaultSecurityTokenHeader,
	}
}

// NewRestrictionService creates a new instance of the restriction service with the provided configurations.
func NewRestrictionService(
	client *req.Client,
	restrictionServiceURL string,
	securityToken string,
	opts ...Option,
) *Service {
	options := defaultServiceOptions()

	for _, opt := range opts {
		opt(options)
	}

	return &Service{
		client:                client,
		restrictionServiceURL: restrictionServiceURL,
		securityToken:         securityToken,
		timeoutExtCall:        options.timeoutExtCall,
		securityTokenHeader:   options.securityTokenHeader,
	}
}

// restrictionErrResponse represents the error response structure from the restriction service.
type restrictionErrResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
