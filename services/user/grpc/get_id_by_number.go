package grpc

import (
	"context"
	"fmt"

	grpcCommon "github.com/whagency/core-common/grpc"
	"github.com/whagency/core-common/grpc/pb/userv1"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/user"
)

// GetUserByNumber retrieves the user ID by its number.
func (s *Service) GetUserByNumber(ctx context.Context, number uint64) (user.User, error) {
	logger.LogCtx(ctx).Debugf("[grpc][Service:GetUserByNumber][number: %d]", number)

	conn, connErr := s.GetConnection()
	if connErr != nil {
		return user.User{}, fmt.Errorf(
			"[grpc][Service:GetUserByNumber][GetConnection][url:%s][err:%w]",
			s.mgr.URL(),
			connErr,
		)
	}

	client := userv1.NewUserServiceClient(conn)

	data, sendErr := client.GetUserByNumber(ctx, &userv1.GetUserByNumberRequest{Number: number})
	if sendErr != nil {
		return user.User{}, grpcCommon.DecodeResponseError(sendErr, "GetUserByNumber")
	}

	decodedData, decodeErr := decodeUserByNumberData(data)
	if decodeErr != nil {
		return user.User{}, fmt.Errorf("[grpc][Service:GetUserByNumber]%w", decodeErr)
	}

	return decodedData, nil
}

// decodeUserByNumberData decodes the gRPC response data into a user.User struct.
func decodeUserByNumberData(data *userv1.GetUserByNumberResponse) (user.User, error) {
	userStatus, userStatusErr := user.NewUserStatusFromString(data.GetStatus())
	if userStatusErr != nil {
		return user.User{}, fmt.Errorf(
			"[decodeUserByNumberData][status:%s][err:%w]",
			data.GetStatus(),
			userStatusErr,
		)
	}

	userVerificationLevel, userVerificationLevelErr := user.NewUserVerificationLevelFromString(data.GetVerificationLevel())
	if userVerificationLevelErr != nil {
		return user.User{}, fmt.Errorf(
			"[decodeUserByNumberData][verificationLevel:%s][err:%w]",
			data.GetVerificationLevel(),
			userVerificationLevelErr,
		)
	}

	return user.User{
		ID:                   data.GetUserId(),
		Number:               data.GetNumber(),
		VerificationLevel:    userVerificationLevel,
		VerificationLevelMax: data.GetVerificationLevelMax(),
		Status:               userStatus,
	}, nil
}
