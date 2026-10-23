package middleware

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/tracer"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	tracerOtel "go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const _maxGinErrorsToRecord = 3

const _redactedHeaderValue = "<redacted>"

var _sensitiveHeadersForLog = map[string]struct{}{
	"authorization":       {},
	"proxy-authorization": {},
	"cookie":              {},
	"set-cookie":          {},
	"x-api-key":           {},
	"x-auth-token":        {},
	"x-session-token":     {},
	"x-csrf-token":        {},
}

// GinObservabilityMiddleware combines logging + tracing into a single Gin middleware.
// It ensures RequestID/InternalTraceID are generated once and used consistently in both logs and traces.
func GinObservabilityMiddleware(opts ...Option) gin.HandlerFunc {
	options := NewOptions(opts...)

	return func(c *gin.Context) {
		// Decide logging policy
		skipLog := isSkippablePath(c.Request.URL.Path, options.LogSkippablePaths...) ||
			isSkippableMethod(c.Request.Method, options.LogSkippableMethods...)

		// Decide tracing policy
		skipTrace := isSkippablePath(c.Request.URL.Path, options.TraceSkippablePaths...) ||
			isSkippableMethod(c.Request.Method, options.TraceSkippableMethods...)

		doLog := !skipLog
		doTrace := tracer.Enabled() && !skipTrace

		// If neither logging nor tracing is needed, exit fast.
		if !doLog && !doTrace {
			c.Next()
			return
		}

		start := time.Now()
		logBody := doLog && logger.IsLoggableBody()

		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}

		// Request ID (from header or new)
		reqID := c.Request.Header.Get(logger.DefaultRequestIDHeader)
		if reqID == "" {
			reqID = uuid.New().String()
		}
		// Internal Trace ID
		internalID := uuid.New().String()

		// Build request-scoped logger (even if doLog=false, it's useful to keep in context)
		reqLogger := logger.Log().With(
			zap.String(logger.ContextKeyRequestID.String(), reqID),
			zap.String(logger.ContextKeyInternalTraceID.String(), internalID),
			zap.String("http_method", c.Request.Method),
			zap.String("http_route", route),
			zap.String("http_target", c.Request.URL.Path),
		)

		// Prepare ctx: inject ids + logger
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, logger.ContextKeyRequestID, reqID)
		ctx = context.WithValue(ctx, logger.ContextKeyInternalTraceID, internalID)
		ctx = context.WithValue(ctx, logger.ContextKeyLogger, reqLogger)

		if doTrace {
			var endTrace func()
			ctx, endTrace = startHTTPGinSpan(
				ctx,
				c,
				"HTTP "+c.Request.Method+" "+route,
				[]attribute.KeyValue{
					attribute.String("http.method", c.Request.Method),
					attribute.String("http.route", route),
					attribute.String("http.target", c.Request.URL.Path),
					attribute.String("request.id", reqID),
					attribute.String("internal.trace_id", internalID),
				},
			)
			defer endTrace()
		}

		// Apply updated context to request
		c.Request = c.Request.WithContext(ctx)

		reqLogger.Infof("[http][middleware:GinObservabilityMiddleware][REST incoming]")

		var respBuf *bytes.Buffer
		if logBody {
			respBuf = prepareGinBodyLogging(c, reqLogger)
		}

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		reqLogger.With(zap.Duration("latency", latency)).
			Infof("[http][middleware:GinObservabilityMiddleware][REST completed][statusCode:%d][dataLength:%d]",
				status, c.Writer.Size(),
			)

		// Context cancellation / deadline
		if c.Request.Context().Err() != nil {
			reqLogger.Errorf(
				"[http][middleware:GinObservabilityMiddleware][REST error in context][statusCode:%d][err:%v]",
				status,
				context.Cause(c.Request.Context()),
			)
		}

		if logBody && respBuf != nil && isResponseBodyLoggable(c) {
			respBody := respBuf.Bytes()
			if int64(len(respBody)) > maxLoggableBodyBytes {
				respBody = respBody[:maxLoggableBodyBytes]
				reqLogger.Warnf("[http][middleware:GinObservabilityMiddleware][responseBody truncated to %d bytes]", maxLoggableBodyBytes)
			}

			if len(respBody) == 0 {
				reqLogger.With(zap.Duration("latency", latency)).
					Infof("[http][middleware:GinObservabilityMiddleware][responseBody:<empty>]")
			} else {
				reqLogger.With(zap.Duration("latency", latency)).
					Infow("[http][middleware:GinObservabilityMiddleware][responseBody]",
						"http_response_body", logger.BodyFieldValueForLog(respBody),
					)
			}
		}
	}
}

// responseGinBodyWriter wraps gin.ResponseWriter to capture response body.
type responseGinBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

// Write implements the gin.ResponseWriter interface for responseGinBodyWriter.
func (w responseGinBodyWriter) Write(b []byte) (int, error) {
	if w.body != nil {
		w.body.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

// prepareGinBodyLogging sets up request/response body capture and logs request body + headers when enabled.
// Returns respBuf to be used later for response body logging.
func prepareGinBodyLogging(c *gin.Context, reqLogger *zap.SugaredLogger) *bytes.Buffer {
	respBuf := &bytes.Buffer{}
	c.Writer = &responseGinBodyWriter{ResponseWriter: c.Writer, body: respBuf}

	// Request body: log only if request looks textual.
	if !isRequestBodyLoggable(c.Request) {
		reqLogger.Infof("[http][middleware:GinObservabilityMiddleware][prepareGinBodyLogging][requestBody:<skipped - non-textual content-type:%q>]", c.Request.Header.Get("Content-Type"))
		reqLogger.Infof("[http][middleware:GinObservabilityMiddleware][prepareGinBodyLogging][requestHeaders:%+v]", sanitizeHeadersForLog(c.Request.Header))
		return respBuf
	}

	// Clone request body (safe: returned bytes are already <= maxLoggableBodyBytes)
	var reqBuf []byte
	if c.Request.Body != nil && c.Request.Body != http.NoBody {
		var (
			err       error
			truncated bool
		)
		reqBuf, c.Request.Body, truncated, err = cloneRequestBody(c.Request.Body)
		if err != nil {
			reqLogger.Errorf("[http][middleware:GinObservabilityMiddleware][prepareGinBodyLogging]%s", err)
			c.Request.Body = http.NoBody
			return respBuf
		}
		if truncated {
			reqLogger.Warnf("[http][middleware:GinObservabilityMiddleware][prepareGinBodyLogging][requestBody truncated to %d bytes]", maxLoggableBodyBytes)
		}
	}

	if len(reqBuf) == 0 {
		reqLogger.Infof("[http][middleware:GinObservabilityMiddleware][prepareGinBodyLogging][requestBody:<empty>]")
	} else {
		reqLogger.Infow("[http][middleware:GinObservabilityMiddleware][prepareGinBodyLogging][requestBody]",
			"http_request_body", logger.BodyFieldValueForLog(reqBuf),
		)
	}

	reqLogger.Infof("[http][middleware:GinObservabilityMiddleware][prepareGinBodyLogging][requestHeaders:%+v]", sanitizeHeadersForLog(c.Request.Header))

	return respBuf
}

// sanitizeHeadersForLog clones headers and redacts sensitive values to prevent credential leakage in logs.
func sanitizeHeadersForLog(headers http.Header) http.Header {
	if headers == nil {
		return http.Header{}
	}

	out := make(http.Header, len(headers))
	for key, values := range headers {
		if _, isSensitive := _sensitiveHeadersForLog[strings.ToLower(key)]; isSensitive {
			out[key] = []string{_redactedHeaderValue}
			continue
		}

		copiedValues := make([]string, len(values))
		copy(copiedValues, values)
		out[key] = copiedValues
	}

	return out
}

// startHTTPGinSpan starts a new OpenTelemetry span for a Gin HTTP request.
func startHTTPGinSpan(
	ctx context.Context,
	c *gin.Context,
	spanName string,
	attrs []attribute.KeyValue,
) (context.Context, func()) {
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(c.Request.Header))

	var (
		span    tracerOtel.Span
		endSpan func(tracer.SpanEndOptions)
	)

	ctx, span, endSpan = tracer.NewSpan(ctx, tracer.SpanOptions{
		Name:       spanName,
		Kind:       tracerOtel.SpanKindServer,
		Attributes: attrs,
	})

	return ctx, func() {
		if endSpan == nil {
			return
		}

		status := c.Writer.Status()
		span.SetAttributes(attribute.Int("http.status_code", status))

		recorded := 0
		lastMsg := ""

		for _, ge := range c.Errors {
			if ge == nil || ge.Err == nil {
				continue
			}
			span.RecordError(ge.Err)
			lastMsg = ge.Err.Error()
			recorded++
			if recorded >= _maxGinErrorsToRecord {
				break
			}
		}

		switch {
		case status >= http.StatusInternalServerError:
			endSpan(tracer.SpanEndOptions{Code: codes.Error, Message: http.StatusText(status)})
		case recorded > 0:
			if lastMsg != "" {
				endSpan(tracer.SpanEndOptions{Code: codes.Error, Message: lastMsg})
			} else {
				endSpan(tracer.SpanEndOptions{Code: codes.Error, Message: "gin errors"})
			}
		default:
			endSpan(tracer.SpanEndOptions{Code: codes.Ok, Message: "OK"})
		}
	}
}
