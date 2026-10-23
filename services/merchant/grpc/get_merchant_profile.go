package grpc

import (
	"context"
	"fmt"

	grpcCommon "github.com/whagency/core-common/grpc"
	"github.com/whagency/core-common/grpc/pb/merchantv1"
	"github.com/whagency/core-common/logger"
	commonMerchant "github.com/whagency/core-common/services/merchant"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const _logPrefixGetMerchantProfile = "[grpc][MerchantService:GetMerchantProfile]"

// GetMerchantProfile resolves reusable merchant profile data via merchant-service gRPC.
func (s *Service) GetMerchantProfile(
	ctx context.Context,
	params commonMerchant.GetMerchantProfileParams,
) (commonMerchant.MerchantProfile, error) {
	logger.LogCtx(ctx).Debugf("%s[merchantID:%s]", _logPrefixGetMerchantProfile, params.MerchantID)

	conn, connErr := s.GetConnection()
	if connErr != nil {
		return commonMerchant.MerchantProfile{}, fmt.Errorf(
			"%s[GetConnection][url:%s][err:%w]",
			_logPrefixGetMerchantProfile,
			s.mgr.URL(),
			connErr,
		)
	}

	client := merchantv1.NewMerchantServiceClient(conn)
	res, sendErr := client.GetMerchantProfile(ctx, &merchantv1.GetMerchantProfileRequest{
		MerchantId: params.MerchantID,
	})
	if sendErr != nil {
		st, ok := status.FromError(sendErr)
		if ok && st.Code() == codes.NotFound {
			return commonMerchant.MerchantProfile{}, fmt.Errorf("%s[err:%w]", _logPrefixGetMerchantProfile, commonMerchant.ErrMerchantNotFound)
		}
		return commonMerchant.MerchantProfile{}, grpcCommon.DecodeResponseError(sendErr, "GetMerchantProfile")
	}

	feeRules, errDecodeFeeRules := decodeFeeRules(res.GetFeeRules())
	if errDecodeFeeRules != nil {
		return commonMerchant.MerchantProfile{}, fmt.Errorf("%s[decodeFeeRules]%w", _logPrefixGetMerchantProfile, errDecodeFeeRules)
	}

	return commonMerchant.MerchantProfile{
		MerchantID:         res.GetMerchantId(),
		MerchantCode:       res.GetMerchantCode(),
		DisplayName:        res.GetDisplayName(),
		MerchantType:       res.GetMerchantType(),
		MerchantStatus:     res.GetMerchantStatus(),
		MerchantURL:        res.GetMerchantUrl(),
		OwnerUserID:        res.GetOwnerUserId(),
		SettlementHoldDays: uint16(res.GetSettlementHoldDays()),
		FeeRules:           feeRules,
	}, nil
}

// decodeFeeRules converts a slice of merchantv1.FeeRule to a slice of commonMerchant.FeeRule.
func decodeFeeRules(values []*merchantv1.FeeRule) ([]commonMerchant.FeeRule, error) {
	if len(values) == 0 {
		return nil, nil
	}

	result := make([]commonMerchant.FeeRule, 0, len(values))
	for i := range values {
		operationType, errDecodeOperationType := decodeOperationType(values[i].GetOperationType())
		if errDecodeOperationType != nil {
			return nil, fmt.Errorf("[decodeFeeRules]%w", errDecodeOperationType)
		}

		feeType, errDecodeFeeType := decodeFeeType(values[i].GetFeeType())
		if errDecodeFeeType != nil {
			return nil, fmt.Errorf("[decodeFeeRules]%w", errDecodeFeeType)
		}

		result = append(result, commonMerchant.FeeRule{
			OperationType: operationType,
			FeeType:       feeType,
			FeeValue:      values[i].GetFeeValue(),
		})
	}

	return result, nil
}

// decodeOperationType converts a merchantv1.OperationType to a commonMerchant.OperationType.
func decodeOperationType(value merchantv1.OperationType) (commonMerchant.OperationType, error) {
	switch value {
	case merchantv1.OperationType_OPERATION_TYPE_PAYMENT:
		return commonMerchant.OperationTypePayment, nil
	case merchantv1.OperationType_OPERATION_TYPE_REFUND:
		return commonMerchant.OperationTypeRefund, nil
	case merchantv1.OperationType_OPERATION_TYPE_PAYOUT:
		return commonMerchant.OperationTypePayout, nil
	case merchantv1.OperationType_OPERATION_TYPE_CHARGEBACK:
		return commonMerchant.OperationTypeChargeback, nil
	default:
		return 0, fmt.Errorf("[decodeOperationType][err: unsupported operation_type: %d]", value)
	}
}

// decodeFeeType converts a merchantv1.FeeType to a commonMerchant.FeeType.
func decodeFeeType(value merchantv1.FeeType) (commonMerchant.FeeType, error) {
	switch value {
	case merchantv1.FeeType_FEE_TYPE_PERCENT:
		return commonMerchant.FeeTypePercent, nil
	case merchantv1.FeeType_FEE_TYPE_FIXED:
		return commonMerchant.FeeTypeFixed, nil
	default:
		return 0, fmt.Errorf("[decodeFeeType][err: unsupported fee_type: %d]", value)
	}
}
