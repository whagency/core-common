package http

import (
	"context"
	"fmt"

	commonHTTP "github.com/whagency/core-common/http"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/restriction"
	"github.com/whagency/core-common/tools"
)

const _checkGeoURL = "/api/v1/internal/restrictions/check-geo"

type checkGeoRequest struct {
	Country       string `json:"country"`
	OperationType uint8  `json:"operationType"`
}

type checkGeoResponse struct {
	Code    int    `json:"code"`    // 0 indicates success, > 0 indicates an error
	Message string `json:"message"` // "success" for successful responses, error message for errors
	Data    struct {
		IsAllow bool `json:"isAllow"`
	} `json:"data"`
}

// IsGeoAllow checks if the operation is allowed in the specified country.
func (s Service) IsGeoAllow(ctx context.Context, country string, operationType restriction.GeoFilterOperationType) (bool, error) {
	logger.LogCtx(ctx).Debugf("[restriction][Service:IsGeoAllow][country:%s][operationType:%d]", country, operationType)

	var (
		response    checkGeoResponse
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
		SetBody(&checkGeoRequest{Country: country, OperationType: operationType.Uint8()}).
		SetSuccessResult(&response).
		SetErrorResult(&errResponse).
		Post(s.restrictionServiceURL + _checkGeoURL)
	if err != nil {
		return false, fmt.Errorf("[restriction][Service:IsGeoAllow][err:%w]", err)
	}
	if !res.IsSuccessState() {
		return false, fmt.Errorf("[restriction][Service:IsGeoAllow][httpCode:%d][appCode:%d][appErrMessage:%s]", res.StatusCode, errResponse.Code, errResponse.Message)
	}

	logger.Log().Debugf("[restriction][Service:IsGeoAllow][response:%+v]", response)

	return response.Data.IsAllow, nil
}
