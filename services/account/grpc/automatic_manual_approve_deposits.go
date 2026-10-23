package grpc

import (
	"context"
	"fmt"

	grpcCommon "github.com/whagency/core-common/grpc"
	"github.com/whagency/core-common/grpc/pb/accountv1"
	"github.com/whagency/core-common/logger"
)

// AutomaticManualApproveDeposits approves all deposit requests that are in manual review for the user.
func (s *Service) AutomaticManualApproveDeposits(ctx context.Context, userId uint64) error {
	logger.LogCtx(ctx).Debugf("[grpc][Service:AutomaticManualApproveDeposits][userId: %d]", userId)

	conn, connErr := s.GetConnection()
	if connErr != nil {
		return fmt.Errorf(
			"[grpc][Service:AutomaticManualApproveDeposits][GetConnection][url:%s][err:%w]",
			s.mgr.URL(),
			connErr,
		)
	}

	client := accountv1.NewAccountServiceClient(conn)

	_, sendErr := client.AutomaticManualApproveDeposits(ctx, &accountv1.AutomaticManualApproveDepositsRequest{UserId: userId})
	if sendErr != nil {
		return grpcCommon.DecodeResponseError(sendErr, "AutomaticManualApproveDeposits")
	}

	return nil
}
