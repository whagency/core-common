package grpc

import (
	"context"
	"fmt"

	grpcCommon "github.com/whagency/core-common/grpc"
	"github.com/whagency/core-common/grpc/pb/restrictionv1"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/restriction"
)

// IsGeoAllow checks if the operation is allowed in the specified country.
func (s *Service) IsGeoAllow(ctx context.Context, country string, operationType restriction.GeoFilterOperationType) (bool, error) {
	logger.LogCtx(ctx).Debugf("[grpc][Service:IsGeoAllow][country:%s][operationType:%d]", country, operationType)

	conn, connErr := s.GetConnection()
	if connErr != nil {
		return false, fmt.Errorf("[grpc][Service:IsGeoAllow][GetConnection][url:%s][err:%w]", s.mgr.URL(), connErr)
	}

	client := restrictionv1.NewRestrictionServiceClient(conn)

	operationTypeEncoded, operationTypeErr := encodeGeoFilterOperationType(operationType)
	if operationTypeErr != nil {
		return false, fmt.Errorf("[grpc][Service:IsGeoAllow]%w", operationTypeErr)
	}

	response, sendErr := client.CheckGeo(ctx, &restrictionv1.CheckGeoRequest{
		Country:       country,
		OperationType: operationTypeEncoded,
	})
	if sendErr != nil {
		return false, grpcCommon.DecodeResponseError(sendErr, "IsGeoAllow")
	}

	return response.GetIsAllow(), nil
}

// encodeGeoFilterOperationType converts a restriction.GeoFilterOperationType to gRPC geo filter operation type.
func encodeGeoFilterOperationType(value restriction.GeoFilterOperationType) (restrictionv1.GeoFilterOperationType, error) {
	switch value {
	case restriction.GeoFilterOperationTypeRegistration:
		return restrictionv1.GeoFilterOperationType_GEO_FILTER_OPERATION_TYPE_REGISTRATION, nil
	case restriction.GeoFilterOperationTypeDeposit:
		return restrictionv1.GeoFilterOperationType_GEO_FILTER_OPERATION_TYPE_DEPOSIT, nil
	case restriction.GeoFilterOperationTypeWithdrawal:
		return restrictionv1.GeoFilterOperationType_GEO_FILTER_OPERATION_TYPE_WITHDRAWAL, nil
	case restriction.GeoFilterOperationTypeSwap:
		return restrictionv1.GeoFilterOperationType_GEO_FILTER_OPERATION_TYPE_SWAP, nil
	case restriction.GeoFilterOperationTypeTransfer:
		return restrictionv1.GeoFilterOperationType_GEO_FILTER_OPERATION_TYPE_TRANSFER, nil
	case restriction.GeoFilterOperationTypeCheckoutPayment:
		return restrictionv1.GeoFilterOperationType_GEO_FILTER_OPERATION_TYPE_CHECKOUT_PAYMENT, nil
	default:
		return 0, fmt.Errorf("[encodeGeoFilterOperationType][err: unsupported geo filter operation_type: %d]", value)
	}
}
