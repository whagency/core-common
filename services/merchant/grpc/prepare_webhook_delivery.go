package grpc

import (
	"context"
	"fmt"
	"strings"

	grpcCommon "github.com/whagency/core-common/grpc"
	"github.com/whagency/core-common/grpc/pb/merchantv1"
	"github.com/whagency/core-common/logger"
	commonMerchant "github.com/whagency/core-common/services/merchant"
	commonTools "github.com/whagency/core-common/tools"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const _logPrefixPrepareWebhookDelivery = "[grpc][MerchantService:PrepareWebhookDelivery]"

// PrepareWebhookDelivery asks merchant-service to resolve webhook config and sign
// the exact raw payload bytes that pay-service will later deliver unchanged.
func (s *Service) PrepareWebhookDelivery(
	ctx context.Context,
	params commonMerchant.PrepareWebhookDeliveryParams,
) (commonMerchant.PreparedWebhookDelivery, error) {
	logger.LogCtx(ctx).Debugf(
		"%s[merchantID:%s][mode:%s][webhookID:%s]",
		_logPrefixPrepareWebhookDelivery,
		params.MerchantID,
		params.Mode.String(),
		params.WebhookID,
	)

	conn, connErr := s.GetConnection()
	if connErr != nil {
		return commonMerchant.PreparedWebhookDelivery{}, fmt.Errorf(
			"%s[GetConnection][url:%s][err:%w]",
			_logPrefixPrepareWebhookDelivery,
			s.mgr.URL(),
			connErr,
		)
	}

	client := merchantv1.NewMerchantServiceClient(conn)
	res, sendErr := client.PrepareWebhookDelivery(ctx, &merchantv1.PrepareWebhookDeliveryRequest{
		MerchantId: params.MerchantID,
		Mode:       params.Mode.Int32(),
		WebhookId:  params.WebhookID,
		RawBody:    params.RawBody,
	})
	if sendErr != nil {
		st, ok := status.FromError(sendErr)
		if ok && st.Code() == codes.NotFound {
			return commonMerchant.PreparedWebhookDelivery{}, fmt.Errorf("%s[err:%w]", _logPrefixPrepareWebhookDelivery, commonMerchant.ErrWebhookConfigNotFound)
		}
		return commonMerchant.PreparedWebhookDelivery{}, grpcCommon.DecodeResponseError(sendErr, "PrepareWebhookDelivery")
	}
	mode, errDecodeMode := commonMerchant.NewModeFromCode(res.GetMode())
	if errDecodeMode != nil {
		return commonMerchant.PreparedWebhookDelivery{}, fmt.Errorf("%s[decodeMode]%w", _logPrefixPrepareWebhookDelivery, errDecodeMode)
	}

	return commonMerchant.PreparedWebhookDelivery{
		MerchantID: res.GetMerchantId(),
		Mode:       mode,
		IsEnabled:  res.GetIsEnabled(),
		WebhookURL: commonTools.TrimmedStringOrNil(res.GetWebhookUrl()),
		Timestamp:  strings.TrimSpace(res.GetTimestamp()),
		Signature:  commonTools.TrimmedStringOrNil(res.GetSignature()),
	}, nil
}
