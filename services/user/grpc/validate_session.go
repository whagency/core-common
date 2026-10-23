package grpc

import (
	"context"
	"fmt"

	grpcCommon "github.com/whagency/core-common/grpc"
	"github.com/whagency/core-common/grpc/pb/userv1"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/user"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ValidateSession validates the session token with the user service and returns the user session details.
func (s *Service) ValidateSession(ctx context.Context, token string, skipExpiredValidation bool) (user.Session, error) {
	logger.LogCtx(ctx).Debugf("[grpc][Service:ValidateSession][tokenProvided:%t][tokenLength:%d]", token != "", len(token))

	conn, connErr := s.GetConnection()
	if connErr != nil {
		return user.Session{}, fmt.Errorf(
			"[grpc][Service:ValidateSession][GetConnection][url:%s][err:%w]",
			s.mgr.URL(),
			connErr,
		)
	}

	client := userv1.NewUserServiceClient(conn)

	data, sendErr := client.ValidateSession(ctx, &userv1.ValidateSessionRequest{Token: token, IsExpiredValidationSkip: skipExpiredValidation})
	if sendErr != nil {
		st, ok := status.FromError(sendErr)
		if ok {
			switch st.Code() {
			case codes.Unauthenticated:
				return user.Session{}, fmt.Errorf("[grpc][Service:ValidateSession][err: %w]", user.ErrSessionExpired)
			case codes.InvalidArgument:
				return user.Session{}, fmt.Errorf("[grpc][Service:ValidateSession][err: %w]", user.ErrSessionInvalid)
			case codes.NotFound:
				return user.Session{}, fmt.Errorf("[grpc][Service:ValidateSession][err: %w]", user.ErrUserNotFound)
			}
		}

		return user.Session{}, grpcCommon.DecodeResponseError(sendErr, "ValidateSession")
	}

	response, errDecode := decodeValidateSessionGrpcData(data)
	if errDecode != nil {
		return user.Session{}, fmt.Errorf("[grpc][Service:ValidateSession][decodeValidateSessionGrpcData]%w", errDecode)
	}

	return response, nil
}

// decodeGrpcData converts grpc response data to user.Session.
func decodeValidateSessionGrpcData(data *userv1.ValidateSessionResponse) (user.Session, error) {
	responseUser := data.GetUser()

	decodedVerificationLevel, err := user.NewUserVerificationLevelFromString(responseUser.GetVerificationLevel())
	if err != nil {
		return user.Session{}, err
	}
	decodedStatus, err := user.NewUserStatusFromString(responseUser.GetStatus())
	if err != nil {
		return user.Session{}, err
	}

	return user.Session{
		SessionID: data.GetSessionId(),
		User: user.User{
			ID:                responseUser.GetId(),
			VerificationLevel: decodedVerificationLevel,
			Country:           responseUser.GetCountry(),
			Timezone:          responseUser.Timezone,
			Locale:            responseUser.Locale,
			Status:            decodedStatus,
		},
	}, nil
}
