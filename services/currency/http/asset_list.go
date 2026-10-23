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

const _getAssetListURL = "/api/v1/assets/list"

const _assetTypeCrypto = "crypto"

type assetListResponse struct {
	Code    int    `json:"code"`    // 0 indicates success, > 0 indicates an error
	Message string `json:"message"` // "success" for successful responses, error message for errors
	Data    struct {
		Assets []assetResponse `json:"assets"`
	} `json:"data"`
}

type assetResponse struct {
	Code      string          `json:"code"`
	AssetType string          `json:"type"`
	Scale     int32           `json:"scale"`
	USDPrice  decimal.Decimal `json:"usdPrice"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// GetAssetMap retrieves the map of assets from the currency service.
func (s Service) GetAssetMap(ctx context.Context) (currency.AssetMap, error) {
	logger.LogCtx(ctx).Debug("[currency][Service:GetAssetMap]")

	var (
		response    assetListResponse
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
		Get(s.currencyServiceURL + _getAssetListURL)
	if err != nil {
		return nil, fmt.Errorf("[currency][Service:GetAssetMap][err:%w]", err)
	}
	if !res.IsSuccessState() {
		return nil, fmt.Errorf("[currency][Service:GetAssetMap][httpCode:%d][appCode:%d][appErrMessage:%s]", res.StatusCode, errResponse.Code, errResponse.Message)
	}

	return response.decode(), nil
}

// decode turns a assetListResponse into domain.AssetMap.
func (r assetListResponse) decode() currency.AssetMap {
	assetMap := make(currency.AssetMap)
	for _, asset := range r.Data.Assets {
		assetCode := currency.NewAssetCode(asset.Code)
		assetType := currency.AssetTypeFiat
		if asset.AssetType == _assetTypeCrypto {
			assetType = currency.AssetTypeCrypto
		}
		assetMap[assetCode] = currency.Asset{
			Code:      assetCode,
			Type:      assetType,
			Scale:     asset.Scale,
			USDPrice:  asset.USDPrice,
			UpdatedAt: asset.UpdatedAt,
		}
	}
	return assetMap
}
