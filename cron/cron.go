package cron

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/tracer"

	"github.com/robfig/cron/v3"
	"go.opentelemetry.io/otel/attribute"
	otelCodes "go.opentelemetry.io/otel/codes"
	otelTrace "go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// FuncWithContext is a cron job function that receives run-scoped context and can return an error.
type FuncWithContext = func(ctx context.Context) error

type Manager struct {
	runner  *cron.Cron
	entries []cron.EntryID
}

// NewManager creates a new cron manager instance
func NewManager() *Manager {
	return &Manager{
		runner: cron.New(),
	}
}

// AddFunc adds a cron job function with the given spec and explicit name to the manager.
func (c *Manager) AddFunc(spec, jobName string, cmd FuncWithContext) error {
	jobName = strings.TrimSpace(jobName)
	if jobName == "" {
		return fmt.Errorf("[cron][Manager::AddFunc][err: jobName is empty]")
	}

	if cmd == nil {
		return fmt.Errorf("[cron][Manager::AddFunc][err: cmd is nil]")
	}

	entryID, err := c.runner.AddFunc(spec, c.wrapWithObservability(spec, jobName, cmd))
	if err != nil {
		return fmt.Errorf("[cron][Manager::AddFunc]%w", err)
	}

	c.entries = append(c.entries, entryID)
	return nil
}

// Start starts to run the cron jobs
func (c *Manager) Start() {
	c.runner.Start()
}

// Stop stops the cron jobs running and removes the entries. A context is returned so the caller can wait for the running jobs to finish
func (c *Manager) Stop() context.Context {
	for i := range c.entries {
		c.runner.Remove(c.entries[i])
	}

	return c.runner.Stop()
}

// wrapWithObservability wraps the given job function with observability
func (c *Manager) wrapWithObservability(spec, jobName string, cmd FuncWithContext) func() {
	return func() {
		startedAt := time.Now()
		ctx := logger.NewInternalLogCtx(context.Background())

		jobLogger := logger.LogCtx(ctx).With(
			zap.String("cron_job", jobName),
			zap.String("cron_spec", spec),
		)
		ctx = context.WithValue(ctx, logger.ContextKeyLogger, jobLogger)

		jobLogger.Infof("[cron][Manager][job:%s][spec:%s][incoming]", jobName, spec)

		var (
			span    otelTrace.Span
			endSpan tracer.SpanEnd
		)
		if tracer.Enabled() {
			attrs := []attribute.KeyValue{
				attribute.String("cron.job", jobName),
				attribute.String("cron.spec", spec),
			}

			if reqID := logger.GetRequestID(ctx); reqID != "" {
				attrs = append(attrs, attribute.String("request.id", reqID))
			}
			if internalTraceID := logger.GetInternalTraceID(ctx); internalTraceID != "" {
				attrs = append(attrs, attribute.String("internal.trace_id", internalTraceID))
			}

			ctx, span, endSpan = tracer.NewSpan(ctx, tracer.SpanOptions{
				Name:       "cron " + jobName,
				Kind:       otelTrace.SpanKindInternal,
				Attributes: attrs,
			})
		}

		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}

			panicErr := fmt.Errorf("panic: %v", recovered)
			latency := time.Since(startedAt)

			jobLogger.With(zap.Duration("latency", latency)).
				Errorf("[cron][Manager][job:%s][spec:%s][panic:%v]", jobName, spec, recovered)

			if span != nil {
				span.RecordError(panicErr)
			}
			if endSpan != nil {
				endSpan(tracer.SpanEndOptions{Code: otelCodes.Error, Message: "panic"})
			}
		}()

		err := cmd(ctx)
		latency := time.Since(startedAt)
		if err != nil {
			jobLogger.With(zap.Duration("latency", latency)).
				Errorf("[cron][Manager][job:%s][spec:%s][err:%v]", jobName, spec, err)

			if span != nil {
				span.RecordError(err)
			}
			if endSpan != nil {
				endSpan(tracer.SpanEndOptions{Code: otelCodes.Error, Message: err.Error()})
			}
			return
		}

		jobLogger.With(zap.Duration("latency", latency)).
			Infof("[cron][Manager][job:%s][spec:%s][completed]", jobName, spec)

		if endSpan != nil {
			endSpan(tracer.SpanEndOptions{Code: otelCodes.Ok, Message: "OK"})
		}
	}
}
