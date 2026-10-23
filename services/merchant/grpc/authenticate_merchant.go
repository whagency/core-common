package grpc

import (
	"context"
	"fmt"

	grpcCommon "github.com/whagency/core-common/grpc"
	"github.com/whagency/core-common/grpc/pb/merchantv1"
	"github.com/whagency/core-common/logger"
	commonMerchant "github.com/whagency/core-common/services/merchant"
	commonTools "github.com/whagency/core-common/tools"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const _logPrefixAuthenticateMerchant = "[grpc][MerchantService:AuthenticateMerchant]"

// AuthenticateMerchant validates merchant signature and credentials via merchant-service.
func (s *Service) AuthenticateMerchant(ctx context.Context, params commonMerchant.AuthenticateMerchantParams) (commonMerchant.AuthenticateMerchantResult, error) {
	logger.LogCtx(ctx).Debugf("%s[APIPublicKey:%s][TimestampMS:%d][HTTPMethod:%s][HTTPPath:%s][ClientIP:%s][ResponseMode:%d]",
		_logPrefixAuthenticateMerchant,
		params.APIPublicKey,
		params.TimestampMS,
		params.HTTPMethod,
		params.HTTPPath,
		params.ClientIP,
		params.ResponseMode,
	)

	conn, connErr := s.GetConnection()
	if connErr != nil {
		return commonMerchant.AuthenticateMerchantResult{}, fmt.Errorf(
			"%s[GetConnection][url:%s][err:%w]",
			_logPrefixAuthenticateMerchant,
			s.mgr.URL(),
			connErr,
		)
	}

	client := merchantv1.NewMerchantServiceClient(conn)
	res, sendErr := client.AuthenticateMerchant(ctx, &merchantv1.AuthenticateMerchantRequest{
		ApiPublicKey: params.APIPublicKey,
		TimestampMs:  params.TimestampMS,
		HttpMethod:   params.HTTPMethod,
		HttpPath:     params.HTTPPath,
		RawBody:      params.RawBody,
		Signature:    params.Signature,
		ClientIp:     params.ClientIP,
		ResponseMode: encodeAuthResponseMode(params.ResponseMode),
	})
	if sendErr != nil {
		return commonMerchant.AuthenticateMerchantResult{}, mapAuthenticateMerchantError(sendErr)
	}

	profile, errDecodeProfile := decodeMerchantProfile(res.GetProfile())
	if errDecodeProfile != nil {
		return commonMerchant.AuthenticateMerchantResult{}, fmt.Errorf("%s[decodeMerchantProfile]%w", _logPrefixAuthenticateMerchant, errDecodeProfile)
	}
	mode, errDecodeMode := commonMerchant.NewModeFromCode(res.GetMode())
	if errDecodeMode != nil {
		return commonMerchant.AuthenticateMerchantResult{}, fmt.Errorf("%s[decodeMode]%w", _logPrefixAuthenticateMerchant, errDecodeMode)
	}

	return commonMerchant.AuthenticateMerchantResult{
		MerchantID:        res.GetMerchantId(),
		Mode:              mode,
		IsEnabled:         res.GetIsEnabled(),
		SuccessURLDefault: commonTools.TrimmedStringOrNil(res.GetSuccessUrlDefault()),
		PendingURLDefault: commonTools.TrimmedStringOrNil(res.GetPendingUrlDefault()),
		FailURLDefault:    commonTools.TrimmedStringOrNil(res.GetFailUrlDefault()),
		Profile:           profile,
	}, nil
}

// mapAuthenticateMerchantError maps transport-level auth error codes to common merchant errors.
func mapAuthenticateMerchantError(err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return grpcCommon.DecodeResponseError(err, "AuthenticateMerchant")
	}

	switch grpcCommon.ExtractErrorInfoReason(err) {
	case commonMerchant.InvalidAPIKeyErrorCode:
		return fmt.Errorf("%s[err:%w]", _logPrefixAuthenticateMerchant, commonMerchant.ErrInvalidAPIKey)
	case commonMerchant.MerchantDisabledErrorCode:
		return fmt.Errorf("%s[err:%w]", _logPrefixAuthenticateMerchant, commonMerchant.ErrMerchantDisabled)
	case commonMerchant.InvalidSignatureErrorCode:
		return fmt.Errorf("%s[err:%w]", _logPrefixAuthenticateMerchant, commonMerchant.ErrInvalidSignature)
	case commonMerchant.TimestampOutOfRangeErrorCode:
		return fmt.Errorf("%s[err:%w]", _logPrefixAuthenticateMerchant, commonMerchant.ErrTimestampOutOfRange)
	case commonMerchant.InvalidIPErrorCode:
		return fmt.Errorf("%s[err:%w]", _logPrefixAuthenticateMerchant, commonMerchant.ErrInvalidIP)
	}

	// Keep fallback mapping by gRPC status code for robustness.
	switch st.Code() {
	case codes.Unauthenticated:
		return fmt.Errorf("%s[err:%w]", _logPrefixAuthenticateMerchant, commonMerchant.ErrInvalidSignature)
	case codes.PermissionDenied:
		return fmt.Errorf("%s[err:%w]", _logPrefixAuthenticateMerchant, commonMerchant.ErrMerchantDisabled)
	case codes.NotFound:
		return fmt.Errorf("%s[err:%w]", _logPrefixAuthenticateMerchant, commonMerchant.ErrMerchantNotFound)
	default:
		return grpcCommon.DecodeResponseError(err, "AuthenticateMerchant")
	}
}

// encodeAuthResponseMode converts domain AuthResponseMode to gRPC auth response mode.
func encodeAuthResponseMode(value commonMerchant.AuthResponseMode) merchantv1.AuthResponseMode {
	switch value {
	case commonMerchant.AuthResponseModeAuthWithProfile:
		return merchantv1.AuthResponseMode_AUTH_RESPONSE_MODE_AUTH_WITH_PROFILE
	default:
		return merchantv1.AuthResponseMode_AUTH_RESPONSE_MODE_AUTH_ONLY
	}
}

// decodeMerchantProfile converts a merchantv1.MerchantProfile to a commonMerchant.MerchantProfile.
func decodeMerchantProfile(value *merchantv1.MerchantProfile) (*commonMerchant.MerchantProfile, error) {
	if value == nil {
		return nil, nil
	}

	feeRules, errDecodeFeeRules := decodeFeeRules(value.GetFeeRules())
	if errDecodeFeeRules != nil {
		return nil, fmt.Errorf("[decodeMerchantProfile]%w", errDecodeFeeRules)
	}

	return &commonMerchant.MerchantProfile{
		MerchantID:         value.GetMerchantId(),
		MerchantCode:       value.GetMerchantCode(),
		DisplayName:        value.GetDisplayName(),
		MerchantType:       value.GetMerchantType(),
		MerchantStatus:     value.GetMerchantStatus(),
		MerchantURL:        value.GetMerchantUrl(),
		OwnerUserID:        value.GetOwnerUserId(),
		SettlementHoldDays: uint16(value.GetSettlementHoldDays()),
		FeeRules:           feeRules,
	}, nil
}
