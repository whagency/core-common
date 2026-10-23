package http

import (
	"context"
	"fmt"

	"github.com/whagency/core-common/errorsx"
	commonHTTP "github.com/whagency/core-common/http"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/user"
	"github.com/whagency/core-common/tools"
)

const _validateSessionURL = "/api/v1/internal/sessions/validate"

type validateSessionRequest struct {
	Token                   string `json:"token"`
	IsExpiredValidationSkip bool   `json:"isExpiredValidationSkip"`
}

type validateSessionUser struct {
	ID                uint64  `json:"id"`
	VerificationLevel string  `json:"verificationLevel"`
	Country           string  `json:"country"`
	Timezone          *string `json:"timezone,omitempty"`
	Locale            *string `json:"locale,omitempty"`
	Status            string  `json:"status"`
}

type validateSessionResponse struct {
	Code    int    `json:"code"`    // 0 indicates success, > 0 indicates an error
	Message string `json:"message"` // "success" for successful responses, error message for errors
	Data    struct {
		SessionID string              `json:"sessionId"`
		User      validateSessionUser `json:"user"`
	} `json:"data"`
}

// ValidateSession validates the session token with the user service and returns the user session details.
func (s Service) ValidateSession(ctx context.Context, token string, skipExpiredValidation bool) (user.Session, error) {
	logger.LogCtx(ctx).Debugf("[user][Service:ValidateSession][tokenProvided:%t][tokenLength:%d]", token != "", len(token))

	var (
		response    validateSessionResponse
		errResponse userErrResponse
	)

	// Bound this call without touching the shared client — see
	// commonHTTP.WithCallTimeout.
	ctx, cancel := commonHTTP.WithCallTimeout(ctx, s.timeoutExtCall)
	defer cancel()

	res, err := s.client.
		R().
		SetContext(ctx).
		SetHeaders(map[string]string{
			tools.AcceptHeader:            tools.JSONContentType,
			logger.DefaultRequestIDHeader: logger.GetRequestID(ctx),
			s.securityTokenHeader:         s.securityToken,
		}).
		SetBody(&validateSessionRequest{
			Token:                   token,
			IsExpiredValidationSkip: skipExpiredValidation,
		}).
		SetSuccessResult(&response).
		SetErrorResult(&errResponse).
		Post(s.userServiceURL + _validateSessionURL)
	if err != nil {
		return user.Session{}, fmt.Errorf("[user][Service:ValidateSession][err:%w]", err)
	}
	if !res.IsSuccessState() {
		switch errResponse.Code {
		case errorsx.UserSessionTokenInvalidError: // Invalid session token
			return user.Session{}, fmt.Errorf("[user][Service:ValidateSession][err: %w]", user.ErrSessionInvalid)
		case errorsx.UserSessionExpiredError: // Session expired
			return user.Session{}, fmt.Errorf("[user][Service:ValidateSession][err: %w]", user.ErrSessionExpired)
		case errorsx.RecordNotFoundError: // User profile not found
			return user.Session{}, fmt.Errorf("[user][Service:ValidateSession][err: %w]", user.ErrUserNotFound)
		default:
			return user.Session{}, fmt.Errorf(
				"[user][Service:ValidateSession][httpCode:%d][appCode:%d][appErrMessage:%s][err: unexpected non-success response]",
				res.StatusCode,
				errResponse.Code,
				errResponse.Message,
			)
		}
	}

	decodedResponse, err := response.decode()
	if err != nil {
		return user.Session{}, fmt.Errorf("[user][Service:ValidateSession]%w", err)
	}

	return decodedResponse, nil
}

// decode turns a validateSessionResponse into user.Session.
func (r validateSessionResponse) decode() (user.Session, error) {
	decodedVerificationLevel, err := user.NewUserVerificationLevelFromString(r.Data.User.VerificationLevel)
	if err != nil {
		return user.Session{}, fmt.Errorf("[user][Service:ValidateSession][decode]%w", err)
	}
	decodedStatus, err := user.NewUserStatusFromString(r.Data.User.Status)
	if err != nil {
		return user.Session{}, fmt.Errorf("[user][Service:ValidateSession][decode]%w", err)
	}

	return user.Session{
		SessionID: r.Data.SessionID,
		User: user.User{
			ID:                r.Data.User.ID,
			VerificationLevel: decodedVerificationLevel,
			Country:           r.Data.User.Country,
			Timezone:          r.Data.User.Timezone,
			Locale:            r.Data.User.Locale,
			Status:            decodedStatus,
		},
	}, nil
}
