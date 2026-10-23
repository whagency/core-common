package http

import (
	"context"
	"fmt"

	commonHTTP "github.com/whagency/core-common/http"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/tools"
)

const _blockUserURL = "/api/v1/internal/users/block"

type blockUserRequest struct {
	UserID uint64 `json:"userId"`
}

// BlockUser blocks the user by its ID with the user service.
func (s Service) BlockUser(ctx context.Context, userID uint64) error {
	logger.LogCtx(ctx).Debugf("[user][Service:BlockUser][userID: %d]", userID)

	var errResponse userErrResponse

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
		SetBody(&blockUserRequest{UserID: userID}).
		SetErrorResult(&errResponse).
		Post(s.userServiceURL + _blockUserURL)
	if err != nil {
		return fmt.Errorf("[user][Service:BlockUser][err: %w]", err)
	}
	if !res.IsSuccessState() {
		return fmt.Errorf("[user][Service:BlockUser][httpCode: %d][appCode: %d][appErrMessage: %s]", res.StatusCode, errResponse.Code, errResponse.Message)
	}

	return nil
}
