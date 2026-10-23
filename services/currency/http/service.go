package currency

import (
	"time"

	"github.com/imroc/req/v3"
	"github.com/whagency/core-common/services"
)

type Options struct {
	timeoutExtCall time.Duration
}

type Option func(*Options)

type Service struct {
	client             *req.Client
	currencyServiceURL string
	timeoutExtCall     time.Duration
}

func WithTimeoutExtCall(timeout time.Duration) Option {
	return func(o *Options) {
		o.timeoutExtCall = timeout
	}
}

func defaultServiceOptions() *Options {
	return &Options{
		timeoutExtCall: services.DefaultTimeoutExtCall,
	}
}

// NewCurrencyService creates a new instance of Service.
func NewCurrencyService(
	client *req.Client,
	currencyServiceURL string,
	opts ...Option,
) *Service {
	options := defaultServiceOptions()

	for _, opt := range opts {
		opt(options)
	}

	return &Service{
		client:             client,
		currencyServiceURL: currencyServiceURL,
		timeoutExtCall:     options.timeoutExtCall,
	}
}

type currencyErrResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
