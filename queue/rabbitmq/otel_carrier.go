package rabbitmq

import (
	"fmt"
	"strings"
)

// rabbitHeadersCarrier adapts RabbitMQ headers (map[string]any) to OpenTelemetry TextMapCarrier.
// Works for W3C TraceContext: "traceparent", "tracestate", plus "baggage".
type rabbitHeadersCarrier struct {
	H map[string]any
}

// Get retrieves a value from this carrier.
func (c rabbitHeadersCarrier) Get(key string) string {
	if c.H == nil {
		return ""
	}

	// Try exact first
	if v, ok := c.H[key]; ok {
		return headerToString(v)
	}

	// Then case-insensitive
	lk := strings.ToLower(key)
	for k, v := range c.H {
		if strings.ToLower(k) == lk {
			return headerToString(v)
		}
	}

	return ""
}

// Set stores a key-value pair in this carrier.
func (c rabbitHeadersCarrier) Set(key string, value string) {
	if c.H == nil {
		return
	}
	c.H[key] = value
}

// Keys returns the list of keys stored in this carrier.
func (c rabbitHeadersCarrier) Keys() []string {
	if c.H == nil {
		return nil
	}
	out := make([]string, 0, len(c.H))
	for k := range c.H {
		out = append(out, k)
	}
	return out
}

// headerToString converts common AMQP header types to string.
func headerToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	case fmt.Stringer:
		return t.String()
	default:
		return ""
	}
}
