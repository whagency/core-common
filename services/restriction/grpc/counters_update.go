package grpc

import (
	"context"
	"fmt"
	"time"

	grpcCommon "github.com/whagency/core-common/grpc"
	"github.com/whagency/core-common/grpc/pb/restrictionv1"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/restriction"
)

// CountersUpdate updates the restriction counters for a user operation.
func (s *Service) CountersUpdate(ctx context.Context, params restriction.CounterUpdateParams) error {
	logger.LogCtx(ctx).Debugf("[grpc][Service:CountersUpdate][params:%+v]", params)

	occurredAtValue := params.OccurredAt.UTC().Format(time.RFC3339)

	conn, connErr := s.GetConnection()
	if connErr != nil {
		return fmt.Errorf("[grpc][Service:CountersUpdate][GetConnection][url:%s][err:%w]", s.mgr.URL(), connErr)
	}

	client := restrictionv1.NewRestrictionServiceClient(conn)

	operationTypeEncoded, operationTypeErr := encodeOperationType(params.OperationType)
	if operationTypeErr != nil {
		return fmt.Errorf("[grpc][Service:CountersUpdate]%w", operationTypeErr)
	}

	if _, sendErr := client.UpdateCounters(ctx, &restrictionv1.UpdateCountersRequest{
		UserId:         params.UserID,
		OperationType:  operationTypeEncoded,
		Amount:         params.Amount.String(),
		AssetCode:      params.AssetCode.String(),
		OccurredAt:     &occurredAtValue,
		IsReverse:      params.IsReverse,
		IdempotencyKey: params.IdempotencyKey,
	}); sendErr != nil {
		return grpcCommon.DecodeResponseError(sendErr, "CountersUpdate")
	}

	return nil
}
