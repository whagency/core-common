package grpc

import (
	"strings"

	"google.golang.org/grpc/metadata"
)

// MetadataCarrier adapts gRPC metadata to the OpenTelemetry TextMapCarrier interface.
type MetadataCarrier struct {
	metadata.MD
}

// NewMetadataCarrier creates a new MetadataCarrier from the given metadata.MD.
func NewMetadataCarrier(md metadata.MD) *MetadataCarrier {
	return &MetadataCarrier{MD: md}
}

// Get retrieves a value from this carrier.
func (mc *MetadataCarrier) Get(key string) string {
	if mc.MD == nil {
		return ""
	}
	values := mc.MD[strings.ToLower(key)]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// Set stores a key-value pair in this carrier.
func (mc *MetadataCarrier) Set(key string, value string) {
	if mc.MD == nil {
		mc.MD = metadata.New(nil)
	}
	mc.MD[strings.ToLower(key)] = []string{value}
}

// Keys lists the keys stored in this carrier.
func (mc *MetadataCarrier) Keys() []string {
	out := make([]string, 0, len(mc.MD))
	for k := range mc.MD {
		out = append(out, k)
	}
	return out
}
