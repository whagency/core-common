package http

import (
	"net/http"

	"github.com/imroc/req/v3"
	"github.com/whagency/core-common/tracer"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// WrapHTTPClient wraps the given HTTP client with OpenTelemetry instrumentation
func WrapHTTPClient(client *http.Client) {
	if client == nil {
		return
	}
	if !tracer.Enabled() {
		return
	}

	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = otelhttp.NewTransport(base)
}

// WrapReqClient wraps req.Client internal *http.Client with OpenTelemetry instrumentation.
func WrapReqClient(client *req.Client) {
	if client == nil {
		return
	}
	WrapHTTPClient(client.GetClient())
}
