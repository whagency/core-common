package currency

import (
	"context"
	"fmt"
	"time"

	commonHTTP "github.com/whagency/core-common/http"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/currency"
	"github.com/whagency/core-common/tools"

	"github.com/shopspring/decimal"
)

const _getRateListURL = "/api/v1/rates/list"

type rateListResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Rates []rateResponse `json:"rates"`
	} `json:"data"`
}

type rateResponse struct {
	CurrencyFrom          string           `json:"currencyFrom"`
	CurrencyTo            string           `json:"currencyTo"`
	Rate                  decimal.Decimal  `json:"rate"`
	ChangePercent         *decimal.Decimal `json:"changePercent"`
	UpdateIntervalSeconds int64            `json:"updateIntervalSeconds"`
	UpdatedAt             time.Time        `json:"updatedAt"`
}

// GetRateMap retrieves the map of rates from the currency service.
func (s Service) GetRateMap(ctx context.Context) (currency.RateMap, error) {
	logger.LogCtx(ctx).Debug("[currency][Service:GetRateMap]")

	var (
		response    rateListResponse
		errResponse currencyErrResponse
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
		}).
		SetSuccessResult(&response).
		SetErrorResult(&errResponse).
		Get(s.currencyServiceURL + _getRateListURL)
	if err != nil {
		return nil, fmt.Errorf("[currency][Service:GetRateMap][err: %w]", err)
	}
	if !res.IsSuccessState() {
		return nil, fmt.Errorf("[currency][Service:GetRateMap][httpCode: %d][appCode: %d][appErrMessage: %s]", res.StatusCode, errResponse.Code, errResponse.Message)
	}

	logger.Log().Debugf("[currency][Service:GetRateMap][response: %+v]", response)

	return response.decode(), nil
}

// decode turns a rateListResponse into domain.RateMap.
func (r rateListResponse) decode() currency.RateMap {
	rateMap := make(currency.RateMap)
	for _, rate := range r.Data.Rates {
		direction := fmt.Sprintf("%s_%s", rate.CurrencyFrom, rate.CurrencyTo)
		rateMap[direction] = currency.Rate{
			CurrencyFrom:          currency.NewAssetCode(rate.CurrencyFrom),
			CurrencyTo:            currency.NewAssetCode(rate.CurrencyTo),
			Rate:                  rate.Rate,
			ChangePercent:         rate.ChangePercent,
			UpdateIntervalSeconds: rate.UpdateIntervalSeconds,
			UpdatedAt:             rate.UpdatedAt,
		}
	}
	return rateMap
}
