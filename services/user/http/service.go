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
	client              *req.Client
	userServiceURL      string
	securityToken       string
	timeoutExtCall      time.Duration
	securityTokenHeader string
}

func WithTimeoutExtCall(timeout time.Duration) Option {
	return func(o *Options) {
		o.timeoutExtCall = timeout
	}
}

func WithSecurityTokenHeader(header string) Option {
	return func(o *Options) {
		o.securityTokenHeader = header
	}
}

func defaultServiceOptions() *Options {
	return &Options{
		timeoutExtCall:      services.DefaultTimeoutExtCall,
		securityTokenHeader: services.DefaultSecurityTokenHeader,
	}
}

func NewUserService(
	client *req.Client,
	userServiceURL string,
	securityToken string,
	opts ...Option,
) *Service {
	options := defaultServiceOptions()

	for _, opt := range opts {
		opt(options)
	}

	return &Service{
		client:              client,
		userServiceURL:      userServiceURL,
		securityToken:       securityToken,
		timeoutExtCall:      options.timeoutExtCall,
		securityTokenHeader: options.securityTokenHeader,
	}
}

type userErrResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
