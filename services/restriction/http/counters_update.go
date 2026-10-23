package http

import (
	"context"
	"fmt"
	"time"

	commonHTTP "github.com/whagency/core-common/http"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/restriction"
	"github.com/whagency/core-common/tools"

	"github.com/shopspring/decimal"
)

const _countersUpdateURL = "/api/v1/internal/restrictions/counters/update"

type countersUpdateRequest struct {
	UserID         uint64          `json:"userId"`
	OperationType  uint8           `json:"operationType"`
	Amount         decimal.Decimal `json:"amount"`
	AssetCode      string          `json:"assetCode"`
	OccurredAt     *time.Time      `json:"occurredAt,omitempty" time_format:"2006-01-02T15:04:05Z07:00"`
	IsReverse      bool            `json:"isReverse"`
	IdempotencyKey *string         `json:"idempotencyKey,omitempty"`
}

// CountersUpdate updates the restriction counters for a user operation.
func (s Service) CountersUpdate(ctx context.Context, params restriction.CounterUpdateParams) error {
	logger.LogCtx(ctx).Debugf("[restriction][Service:CountersUpdate][params:%+v]", params)

	var errResponse restrictionErrResponse

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
		SetBody(&countersUpdateRequest{
			UserID:         params.UserID,
			OperationType:  params.OperationType.Uint8(),
			Amount:         params.Amount,
			AssetCode:      params.AssetCode.String(),
			IsReverse:      params.IsReverse,
			IdempotencyKey: params.IdempotencyKey,
		}).
		SetErrorResult(&errResponse).
		Post(s.restrictionServiceURL + _countersUpdateURL)
	if err != nil {
		return fmt.Errorf("[restriction][Service:CountersUpdate][err:%w]", err)
	}
	if !res.IsSuccessState() {
		return fmt.Errorf("[restriction][Service:CountersUpdate][httpCode:%d][appCode:%d][appErrMessage:%s]", res.StatusCode, errResponse.Code, errResponse.Message)
	}

	return nil
}
