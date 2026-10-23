package grpc

import (
	"context"
	"fmt"

	grpcCommon "github.com/whagency/core-common/grpc"
	"github.com/whagency/core-common/grpc/pb/restrictionv1"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/restriction"
)

// CheckLimit checks if the user operation is within allowed limits.
func (s *Service) CheckLimit(ctx context.Context, params restriction.CheckLimitParams) (restriction.CheckLimitResult, error) {
	logger.LogCtx(ctx).Debugf("[grpc][Service:CheckLimit][params:%+v]", params)

	conn, connErr := s.GetConnection()
	if connErr != nil {
		return restriction.CheckLimitResult{}, fmt.Errorf("[grpc][Service:CheckLimit][GetConnection][url:%s][err:%w]", s.mgr.URL(), connErr)
	}

	client := restrictionv1.NewRestrictionServiceClient(conn)

	operationTypeEncoded, operationTypeErr := encodeOperationType(params.OperationType)
	if operationTypeErr != nil {
		return restriction.CheckLimitResult{}, fmt.Errorf("[grpc][Service:CheckLimit]%w", operationTypeErr)
	}

	request := &restrictionv1.CheckLimitRequest{
		UserId:        params.UserID,
		OperationType: operationTypeEncoded,
		Amount:        params.Amount.String(),
		AssetCode:     params.AssetCode.String(),
	}
	if params.Country != "" {
		request.Country = &params.Country
	}

	response, sendErr := client.CheckLimit(ctx, request)
	if sendErr != nil {
		return restriction.CheckLimitResult{}, grpcCommon.DecodeResponseError(sendErr, "CheckLimit")
	}

	decodedDecisionType, errDecode := restriction.NewDecisionTypeFromString(response.GetDecision())
	if errDecode != nil {
		return restriction.CheckLimitResult{}, fmt.Errorf("[grpc][Service:CheckLimit]%w", errDecode)
	}

	var reason *string
	if response.ReasonCode != nil {
		reason = response.ReasonCode
	}

	return restriction.CheckLimitResult{
		OperationType: params.OperationType,
		Decision:      decodedDecisionType,
		Reason:        reason,
	}, nil
}
