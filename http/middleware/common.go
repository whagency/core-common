package middleware

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/tracer"
)

const maxLoggableBodyBytes int64 = 1 << 20 // 1 MiB

var (
	// defaultSkippablePaths contains paths that are skippable by default in the middleware.
	defaultSkippablePaths = []string{
		"metrics",
		"debug/pprof",
		"swagger",
		"health",
	}

	// defaultSkippableMethods contains HTTP methods that are skippable by default in the middleware.
	defaultSkippableMethods = []string{
		http.MethodOptions,
	}
)

type Options struct {
	// LogSkippable* controls what requests should NOT be logged.
	LogSkippablePaths   []string
	LogSkippableMethods []string

	// TraceSkippable* controls what requests should NOT be traced.
	TraceSkippablePaths   []string
	TraceSkippableMethods []string
}
type Option func(*Options)

// NewOptions creates a new Options instance with default skippable paths and methods.
func NewOptions(opts ...Option) *Options {
	o := &Options{
		LogSkippablePaths:     make([]string, len(defaultSkippablePaths)),
		LogSkippableMethods:   make([]string, len(defaultSkippableMethods)),
		TraceSkippablePaths:   make([]string, len(defaultSkippablePaths)),
		TraceSkippableMethods: make([]string, len(defaultSkippableMethods)),
	}

	copy(o.LogSkippablePaths, defaultSkippablePaths)
	copy(o.LogSkippableMethods, defaultSkippableMethods)
	copy(o.TraceSkippablePaths, defaultSkippablePaths)
	copy(o.TraceSkippableMethods, defaultSkippableMethods)

	if cfgLogPaths := logger.GetConfig().GetHTTPNotLoggablePaths(); len(cfgLogPaths) > 0 {
		o.LogSkippablePaths = append(o.LogSkippablePaths, cfgLogPaths...)
	}

	if cfgTracePaths := tracer.GetConfig().GetHTTPNotTraceablePaths(); len(cfgTracePaths) > 0 {
		o.TraceSkippablePaths = append(o.TraceSkippablePaths, cfgTracePaths...)
	}

	for _, apply := range opts {
		apply(o)
	}
	return o
}

// WithSkippablePaths adds additional paths to the list of skippable paths.
func WithSkippablePaths(paths ...string) Option {
	return func(o *Options) {
		o.LogSkippablePaths = append(o.LogSkippablePaths, paths...)
		o.TraceSkippablePaths = append(o.TraceSkippablePaths, paths...)
	}
}

// WithSkippableMethods adds additional HTTP methods to the list of skippable methods.
func WithSkippableMethods(methods ...string) Option {
	return func(o *Options) {
		o.LogSkippableMethods = append(o.LogSkippableMethods, methods...)
		o.TraceSkippableMethods = append(o.TraceSkippableMethods, methods...)
	}
}

// WithLogSkippablePaths adds additional paths to be skipped by LOGGING only.
func WithLogSkippablePaths(paths ...string) Option {
	return func(o *Options) {
		o.LogSkippablePaths = append(o.LogSkippablePaths, paths...)
	}
}

// WithLogSkippableMethods adds additional methods to be skipped by LOGGING only.
func WithLogSkippableMethods(methods ...string) Option {
	return func(o *Options) {
		o.LogSkippableMethods = append(o.LogSkippableMethods, methods...)
	}
}

// WithTraceSkippablePaths adds additional paths to be skipped by TRACING only.
func WithTraceSkippablePaths(paths ...string) Option {
	return func(o *Options) {
		o.TraceSkippablePaths = append(o.TraceSkippablePaths, paths...)
	}
}

// WithTraceSkippableMethods adds additional methods to be skipped by TRACING only.
func WithTraceSkippableMethods(methods ...string) Option {
	return func(o *Options) {
		o.TraceSkippableMethods = append(o.TraceSkippableMethods, methods...)
	}
}

// isSkippableMethod checks if the given HTTP method is skippable based on default methods and any extra methods provided.
func isSkippableMethod(method string, skip ...string) bool {
	for _, m := range skip {
		if method == m {
			return true
		}
	}
	return false
}

// isSkippablePath checks if the given path is skippable based on default paths and any extra paths provided.
func isSkippablePath(path string, skip ...string) bool {
	t := strings.TrimPrefix(path, "/")
	for _, pre := range skip {
		if strings.HasPrefix(t, strings.TrimPrefix(pre, "/")) {
			return true
		}
	}
	return false
}

// multiReadCloser is a helper struct that combines an io.Reader and an io.Closer.
type multiReadCloser struct {
	io.Reader
	closer io.Closer
}

// Close closes the underlying closer if it exists.
func (m *multiReadCloser) Close() error {
	if m.closer != nil {
		return m.closer.Close()
	}
	return nil
}

// cloneRequestBody reads up to maxLoggableBodyBytes+1 bytes to detect truncation.
// It returns:
//  1. bodyForLog: at most maxLoggableBodyBytes bytes (safe for logging)
//  2. newBody: a ReadCloser that yields the already-read bytes + the remaining original stream
//  3. truncated: whether the original body exceeded maxLoggableBodyBytes
func cloneRequestBody(body io.ReadCloser) ([]byte, io.ReadCloser, bool, error) {
	if body == nil || body == http.NoBody {
		return nil, http.NoBody, false, nil
	}

	// read at most max+1 so we can detect truncation
	limited := io.LimitReader(body, maxLoggableBodyBytes+1)

	buf, err := io.ReadAll(limited)
	if err != nil {
		_ = body.Close()
		return nil, http.NoBody, false, fmt.Errorf("[cloneRequestBody][err:%w]", err)
	}

	truncated := int64(len(buf)) > maxLoggableBodyBytes

	// keep a safe slice for logging (max bytes)
	bodyForLog := buf
	if truncated {
		bodyForLog = buf[:maxLoggableBodyBytes]
	}

	// Downstream must see the full consumed bytes (including the +1 if present),
	// otherwise we'd "eat" a byte from the original body.
	newBody := &multiReadCloser{
		Reader: io.MultiReader(bytes.NewReader(buf), body),
		closer: body,
	}

	return bodyForLog, newBody, truncated, nil
}

// isRequestBodyLoggable returns true if request body is likely textual (safe to log).
func isRequestBodyLoggable(r *http.Request) bool {
	if r == nil {
		return false
	}
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		// unknown: be conservative and do not log (binary uploads often omit/override weirdly)
		return false
	}
	return isTextualContentType(ct)
}

// isResponseBodyLoggable response body should be logged only if response looks textual and not compressed.
func isResponseBodyLoggable(c *gin.Context) bool {
	return isTextualContentType(c.Writer.Header().Get("Content-Type")) && !isCompressed(c.Writer.Header().Get("Content-Encoding"))
}

// isTextualContentType a minimal filter: allow json/text/xml/x-www-form-urlencoded, block multipart/octet-stream etc.
func isTextualContentType(ct string) bool {
	ct = strings.ToLower(ct)
	if ct == "" {
		// if unknown, be conservative and do not log (could be binary content)
		return false
	}

	switch {
	case strings.HasPrefix(ct, "application/json"):
		return true
	case strings.HasPrefix(ct, "application/xml"):
		return true
	case strings.HasPrefix(ct, "text/"):
		return true
	case strings.HasPrefix(ct, "application/x-www-form-urlencoded"):
		return true
	default:
		return false
	}
}

// isCompressed checks if the content encoding is compressed (gzip, br, deflate, zstd, etc.)
func isCompressed(enc string) bool {
	enc = strings.ToLower(strings.TrimSpace(enc))
	if enc == "" || enc == "identity" {
		return false
	}
	// gzip, br, deflate, zstd, etc.
	return true
}
