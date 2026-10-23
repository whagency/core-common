package middlewares

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/http"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/user"

	"github.com/gin-gonic/gin"
)

// _authenticatedSessionContextKey Store in both request context and Gin context for convenience.
const _authenticatedSessionContextKey = "authenticatedSession"

type _validateUserSessionConfig struct {
	skipExpiredValidation bool
}

// ValidateUserSessionOption configures ValidateUserSession behavior.
type ValidateUserSessionOption func(*_validateUserSessionConfig)

// WithSkipExpiredValidation configures whether expired sessions should be skipped during validation.
func WithSkipExpiredValidation(skip bool) ValidateUserSessionOption {
	return func(cfg *_validateUserSessionConfig) {
		cfg.skipExpiredValidation = skip
	}
}

// ValidateUserSession validates the user session
func ValidateUserSession(service user.Service, opts ...ValidateUserSessionOption) gin.HandlerFunc {
	cfg := _validateUserSessionConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	return func(c *gin.Context) {
		ctx := c.Request.Context()

		authToken := strings.TrimSpace(c.GetHeader("Authorization"))
		if authToken == "" {
			er := errorsx.New(
				errorsx.UserSessionTokenInvalidError,
				errorsx.WithCause(fmt.Errorf("[middleware:ValidateUserSession][err: missing authorization header]")),
			)
			c.AbortWithStatusJSON(er.HTTPStatus, http.EncodeErrorResponse(ctx, er))
			return
		}

		userSession, err := service.ValidateSession(ctx, authToken, cfg.skipExpiredValidation)
		if err != nil {
			er := errorsx.New(
				decodeErrCodeBasedOnUserSessionError(err),
				errorsx.WithCause(fmt.Errorf("[middleware:ValidateUserSession]%w", err)),
			)
			c.AbortWithStatusJSON(er.HTTPStatus, http.EncodeErrorResponse(ctx, er))
			return
		}

		// Attach to request context and Gin context
		ctxWithSession := context.WithValue(ctx, user.ContextAuthenticatedSessionKey{}, userSession)
		c.Request = c.Request.WithContext(ctxWithSession)
		c.Set(_authenticatedSessionContextKey, userSession)

		c.Next()
	}
}

// decodeErrCodeBasedOnUserSessionError decodes the error code based on the type of user session error.
func decodeErrCodeBasedOnUserSessionError(err error) int {
	switch {
	case errors.Is(err, user.ErrSessionExpired):
		return errorsx.UserSessionExpiredError
	case errors.Is(err, user.ErrSessionInvalid), errors.Is(err, user.ErrUserNotFound):
		return errorsx.UserSessionTokenInvalidError
	default:
		return errorsx.InternalServerError
	}
}

// RequireAuthenticatedSession extracts the authenticated session from middleware state.
// If the session is missing, a standardized 500 error response is produced and the handler must stop.
func RequireAuthenticatedSession(c *gin.Context, logPrefix string) (user.Session, bool) {
	ctx := c.Request.Context()

	session, ok := authenticatedSessionFromContext(c)
	if ok {
		return session, true
	}

	causeMessage := "[err: missing authenticated session context]"
	if logPrefix != "" {
		causeMessage = fmt.Sprintf("%s%s", logPrefix, causeMessage)
	}

	er := errorsx.New(
		errorsx.InternalServerError,
		errorsx.WithCause(errors.New(causeMessage)),
	)
	logger.LogCtx(ctx).Error(er.Cause)
	c.AbortWithStatusJSON(er.HTTPStatus, http.EncodeErrorResponse(ctx, er))
	return user.Session{}, false
}

// authenticatedSessionFromContext retrieves the authenticated session from gin context
// or from the request context, whichever is available.
func authenticatedSessionFromContext(c *gin.Context) (user.Session, bool) {
	if raw, ok := c.Get(_authenticatedSessionContextKey); ok {
		if s, isValid := raw.(user.Session); isValid {
			return s, true
		}
	}
	if raw := c.Request.Context().Value(user.ContextAuthenticatedSessionKey{}); raw != nil {
		if s, ok := raw.(user.Session); ok {
			return s, true
		}
	}
	return user.Session{}, false
}
