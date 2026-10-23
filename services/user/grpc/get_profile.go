package grpc

import (
	"context"
	"fmt"

	grpcCommon "github.com/whagency/core-common/grpc"
	"github.com/whagency/core-common/grpc/pb/userv1"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/user"
)

// GetProfile retrieves the profile of the user.
func (s *Service) GetProfile(ctx context.Context, userId uint64) (user.Profile, error) {
	logger.LogCtx(ctx).Debugf("[grpc][Service:GetProfile][userId: %d]", userId)

	conn, connErr := s.GetConnection()
	if connErr != nil {
		return user.Profile{}, fmt.Errorf(
			"[grpc][Service:GetProfile][GetConnection][url:%s][err:%w]",
			s.mgr.URL(),
			connErr,
		)
	}

	client := userv1.NewUserServiceClient(conn)

	data, sendErr := client.GetProfile(ctx, &userv1.GetProfileRequest{UserId: userId})
	if sendErr != nil {
		return user.Profile{}, grpcCommon.DecodeResponseError(sendErr, "GetProfile")
	}

	return decodeGrpcData(data), nil
}

// decodeGrpcData converts grpc response data to domainGrpc.UserProfile
func decodeGrpcData(data *userv1.GetProfileResponse) user.Profile {
	var contactType user.ContactType
	switch data.GetContactType() {
	case userv1.ContactType_CONTACT_TYPE_EMAIL:
		contactType = user.ContactTypeEmail
	case userv1.ContactType_CONTACT_TYPE_PHONE:
		contactType = user.ContactTypePhone
	default:
		contactType = user.ContactTypeEmail
	}

	return user.Profile{
		Locale:       data.GetLocale(),
		ContactType:  contactType,
		ContactValue: data.GetContactValue(),
	}
}
