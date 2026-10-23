package http

import (
	"context"
	"fmt"

	commonHTTP "github.com/whagency/core-common/http"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/restriction"
	"github.com/whagency/core-common/tools"

	"github.com/shopspring/decimal"
)

const _checkLimitURL = "/api/v1/internal/restrictions/check-limit"

type checkLimitRequest struct {
	UserID        uint64          `json:"userId"`
	OperationType uint8           `json:"operationType"`
	Amount        decimal.Decimal `json:"amount"`
	AssetCode     string          `json:"assetCode"`
	Country       string          `json:"country"`
}

type checkLimitResponse struct {
	Code    int    `json:"code"`    // 0 indicates success, > 0 indicates an error
	Message string `json:"message"` // "success" for successful responses, error message for errors
	Data    struct {
		Decision   string  `json:"decision"`
		ReasonCode *string `json:"reasonCode,omitempty"`
	} `json:"data"`
}

// CheckLimit checks if the user operation is within allowed limits.
func (s Service) CheckLimit(ctx context.Context, params restriction.CheckLimitParams) (restriction.CheckLimitResult, error) {
	logger.LogCtx(ctx).Debugf("[restriction][Service:CheckLimit][params:%+v]", params)

	var (
		response    checkLimitResponse
		errResponse restrictionErrResponse
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
		SetBody(&checkLimitRequest{
			UserID:        params.UserID,
			OperationType: params.OperationType.Uint8(),
			Amount:        params.Amount,
			AssetCode:     params.AssetCode.String(),
			Country:       params.Country,
		}).
		SetSuccessResult(&response).
		SetErrorResult(&errResponse).
		Post(s.restrictionServiceURL + _checkLimitURL)
	if err != nil {
		return restriction.CheckLimitResult{}, fmt.Errorf("[restriction][Service:CheckLimit][err:%w]", err)
	}
	if !res.IsSuccessState() {
		return restriction.CheckLimitResult{}, fmt.Errorf("[restriction][Service:CheckLimit][httpCode:%d][appCode:%d][appErrMessage:%s]", res.StatusCode, errResponse.Code, errResponse.Message)
	}

	logger.Log().Debugf("[restriction][Service:CheckLimit][response:%+v]", response)

	decodedDecisionType, errDecode := restriction.NewDecisionTypeFromString(response.Data.Decision)
	if errDecode != nil {
		return restriction.CheckLimitResult{}, fmt.Errorf("[restriction][Service:CheckLimit]%w", errDecode)
	}

	return restriction.CheckLimitResult{
		OperationType: params.OperationType,
		Decision:      decodedDecisionType,
		Reason:        response.Data.ReasonCode,
	}, nil
}
