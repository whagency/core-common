package grpc

import (
	"context"
	"fmt"
	"strings"

	appErrors "github.com/whagency/core-common/errorsx"
	grpcCommon "github.com/whagency/core-common/grpc"
	"github.com/whagency/core-common/grpc/pb/accountv1"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/account"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MakeTransaction create new transaction for selected user.
func (s *Service) MakeTransaction(ctx context.Context, params account.MakeTransactionParams) (account.MakeTransactionResponse, error) {
	logger.LogCtx(ctx).Debugf("[grpc][Service:MakeTransaction][params:%+v]", params)

	conn, connErr := s.GetConnection()
	if connErr != nil {
		return account.MakeTransactionResponse{}, fmt.Errorf(
			"[grpc][Service:MakeTransaction][GetConnection][url:%s][err:%w]",
			s.mgr.URL(),
			connErr,
		)
	}

	client := accountv1.NewAccountServiceClient(conn)

	requestType, requestTypeErr := encodeJournalBizType(params.Type)
	if requestTypeErr != nil {
		return account.MakeTransactionResponse{}, fmt.Errorf("[grpc][Service:MakeTransaction]%w", requestTypeErr)
	}

	requestFee := params.Fee.String()

	data, sendErr := client.MakeTransaction(ctx, &accountv1.MakeTransactionRequest{
		UserId:         params.UserID,
		Type:           requestType,
		ToUserId:       params.ToUserID,
		AssetCode:      params.AssetCode.String(),
		Amount:         params.Amount.String(),
		Fee:            &requestFee,
		IdempotencyKey: params.IdempotencyKey,
		Description:    params.Description,
	})
	if sendErr != nil {
		return account.MakeTransactionResponse{}, mapMakeTransactionError(sendErr)
	}

	return decodeGrpcData(data), nil
}

// mapMakeTransactionError maps transport-level account errors to typed account domain errors.
func mapMakeTransactionError(err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return grpcCommon.DecodeResponseError(err, "MakeTransaction")
	}

	switch grpcCommon.ExtractErrorInfoReason(err) {
	case account.InsufficientFundsErrorCode:
		return fmt.Errorf("[grpc][Service:MakeTransaction][err:%w]", account.ErrInsufficientFunds)
	case account.RestrictedOperationErrorCode:
		return fmt.Errorf("[grpc][Service:MakeTransaction][err:%w]", account.ErrRestrictedOperation)
	}

	switch st.Code() {
	case codes.PermissionDenied:
		return fmt.Errorf("[grpc][Service:MakeTransaction][err:%w]", account.ErrRestrictedOperation)
	case codes.InvalidArgument:
		lowerMessage := strings.ToLower(st.Message())
		insufficientFundsMessage := strings.ToLower(appErrors.InsufficientFundsErrorMessage)
		if strings.Contains(lowerMessage, insufficientFundsMessage) {
			return fmt.Errorf("[grpc][Service:MakeTransaction][err:%w]", account.ErrInsufficientFunds)
		}

		restrictedOperationMessage := strings.ToLower(appErrors.RestrictedOperationErrMessage)
		if strings.Contains(lowerMessage, restrictedOperationMessage) {
			return fmt.Errorf("[grpc][Service:MakeTransaction][err:%w]", account.ErrRestrictedOperation)
		}
	}

	return grpcCommon.DecodeResponseError(err, "MakeTransaction")
}

// encodeJournalBizType converts account.JournalBizType grpc request type
func encodeJournalBizType(journalBizType account.JournalBizType) (accountv1.JournalBizType, error) {
	switch journalBizType {
	case account.JournalBizTypeCheckoutPayment:
		return accountv1.JournalBizType_JOURNAL_BIZ_TYPE_CHECKOUT_PAYMENT, nil
	case account.JournalBizTypeCheckoutPaymentHold:
		return accountv1.JournalBizType_JOURNAL_BIZ_TYPE_CHECKOUT_PAYMENT_HOLD, nil
	case account.JournalBizTypeCheckoutPaymentHoldRelease:
		return accountv1.JournalBizType_JOURNAL_BIZ_TYPE_CHECKOUT_PAYMENT_HOLD_RELEASE, nil
	default:
		return accountv1.JournalBizType_JOURNAL_BIZ_TYPE_UNSPECIFIED, fmt.Errorf(
			"[encodeJournalBizType][err: unsupported journal biz type for current account proto: %s]",
			journalBizType.String(),
		)
	}
}

// decodeGrpcData converts grpc response data to account.MakeTransactionResponse
func decodeGrpcData(data *accountv1.MakeTransactionResponse) account.MakeTransactionResponse {
	return account.MakeTransactionResponse{
		JournalID: data.GetJournalId(),
		Status:    decodeJournalStatus(data.GetStatus()),
	}
}

// decodeJournalStatus converts the protobuf journal status to its domain value.
func decodeJournalStatus(status accountv1.JournalStatus) account.JournalStatus {
	switch status {
	case accountv1.JournalStatus_JOURNAL_STATUS_PENDING:
		return account.JournalStatusPending
	case accountv1.JournalStatus_JOURNAL_STATUS_COMPLETED:
		return account.JournalStatusCompleted
	case accountv1.JournalStatus_JOURNAL_STATUS_FAILED:
		return account.JournalStatusFailed
	case accountv1.JournalStatus_JOURNAL_STATUS_CANCELLED:
		return account.JournalStatusCancelled
	case accountv1.JournalStatus_JOURNAL_STATUS_REVERSED:
		return account.JournalStatusReversed
	default:
		return account.JournalStatusUnspecified
	}
}
