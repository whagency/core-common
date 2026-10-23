package grpc

import (
	"context"
	"fmt"

	grpcCommon "github.com/whagency/core-common/grpc"
	"github.com/whagency/core-common/grpc/pb/restrictionv1"
	"github.com/whagency/core-common/logger"
)

// SetRequiredVerification enables or disables is required verification field in the tier for a user.
func (s *Service) SetRequiredVerification(ctx context.Context, userId uint64, active bool) error {
	logger.LogCtx(ctx).Debugf("[grpc][Service:SetRequiredVerification][userId: %d][active: %t]", userId, active)

	conn, connErr := s.GetConnection()
	if connErr != nil {
		return fmt.Errorf(
			"[grpc][Service:SetRequiredVerification][GetConnection][url:%s][err:%w]",
			s.mgr.URL(),
			connErr,
		)
	}

	client := restrictionv1.NewRestrictionServiceClient(conn)

	_, sendErr := client.SetRequiredVerification(ctx, &restrictionv1.SetRequiredVerificationRequest{UserId: userId, Active: active})
	if sendErr != nil {
		return grpcCommon.DecodeResponseError(sendErr, "SetRequiredVerification")
	}

	return nil
}
