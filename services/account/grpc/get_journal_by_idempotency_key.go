package grpc

import (
	"context"
	"fmt"
	"strings"

	grpcCommon "github.com/whagency/core-common/grpc"
	"github.com/whagency/core-common/grpc/pb/accountv1"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/account"
)

// GetJournalByIdempotencyKey looks up a journal entry without creating one.
//
// This is the read-only counterpart to MakeTransaction's idempotency check.
// MakeTransaction returns an existing journal for a matching key but creates one
// when there is no match, so it cannot be used to ask whether a posting exists —
// asking would create it. Compensation and reconciliation flows need this call
// instead.
//
// A journal that was already rolled back is reported as not found: the rollback
// renames the original key so the business operation can be re-attempted. Callers
// asking "is there a live posting to compensate?" should treat not-found as
// "nothing to do", which is correct both for a posting that never happened and
// for one that has already been reversed.
func (s *Service) GetJournalByIdempotencyKey(
	ctx context.Context,
	params account.GetJournalByIdempotencyKeyParams,
) (account.GetJournalByIdempotencyKeyResult, error) {
	logger.LogCtx(ctx).Debugf("[grpc][Service:GetJournalByIdempotencyKey][params:%+v]", params)

	key := strings.TrimSpace(params.IdempotencyKey)
	if key == "" {
		return account.GetJournalByIdempotencyKeyResult{}, fmt.Errorf(
			"[grpc][Service:GetJournalByIdempotencyKey][err: idempotency key is required]",
		)
	}

	conn, connErr := s.GetConnection()
	if connErr != nil {
		return account.GetJournalByIdempotencyKeyResult{}, fmt.Errorf(
			"[grpc][Service:GetJournalByIdempotencyKey][GetConnection][url:%s][err:%w]",
			s.mgr.URL(),
			connErr,
		)
	}

	requestType, requestTypeErr := encodeJournalBizType(params.Type)
	if requestTypeErr != nil {
		return account.GetJournalByIdempotencyKeyResult{}, fmt.Errorf(
			"[grpc][Service:GetJournalByIdempotencyKey]%w",
			requestTypeErr,
		)
	}

	client := accountv1.NewAccountServiceClient(conn)

	data, sendErr := client.GetJournalByIdempotencyKey(ctx, &accountv1.GetJournalByIdempotencyKeyRequest{
		Type:           requestType,
		IdempotencyKey: key,
	})
	if sendErr != nil {
		return account.GetJournalByIdempotencyKeyResult{}, grpcCommon.DecodeResponseError(sendErr, "GetJournalByIdempotencyKey")
	}

	return account.GetJournalByIdempotencyKeyResult{
		Found:     data.GetFound(),
		JournalID: data.GetJournalId(),
		Status:    decodeJournalStatus(data.GetStatus()),
	}, nil
}
