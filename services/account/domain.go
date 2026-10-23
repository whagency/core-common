package account

import (
	"errors"

	"github.com/whagency/core-common/services/currency"

	"github.com/shopspring/decimal"
)

const (
	InsufficientFundsErrorCode   = "insufficient_funds"
	RestrictedOperationErrorCode = "operation_restricted"
)

var (
	ErrInsufficientFunds   = errors.New(InsufficientFundsErrorCode)
	ErrRestrictedOperation = errors.New(RestrictedOperationErrorCode)
)

// JournalStatus describes the lifecycle of a journal entry.
type JournalStatus uint8

// NewJournalStatus creates a new JournalStatus from a uint8 value.
func NewJournalStatus(status uint8) JournalStatus {
	return JournalStatus(status)
}

const (
	JournalStatusUnspecified JournalStatus = iota
	JournalStatusPending
	JournalStatusCompleted
	JournalStatusFailed
	JournalStatusCancelled
	JournalStatusReversed
)

var _journalStatusToString = map[JournalStatus]string{
	JournalStatusUnspecified: "unspecified",
	JournalStatusPending:     "pending",
	JournalStatusCompleted:   "completed",
	JournalStatusFailed:      "failed",
	JournalStatusCancelled:   "cancelled",
	JournalStatusReversed:    "reversed",
}

// String returns the string representation of the JournalStatus.
func (j JournalStatus) String() string {
	if s, ok := _journalStatusToString[j]; ok {
		return s
	}
	return "unknown"
}

// Uint8 returns the uint8 representation of the JournalStatus.
func (j JournalStatus) Uint8() uint8 {
	return uint8(j)
}

type JournalBizType uint8

// NewJournalBizType creates a new JournalBizType from a uint8 value.
func NewJournalBizType(bizType uint8) JournalBizType {
	return JournalBizType(bizType)
}

// Uint8 returns the uint8 representation of the JournalBizType.
func (j JournalBizType) Uint8() uint8 {
	return uint8(j)
}

const (
	JournalBizTypeDeposit JournalBizType = iota + 1
	JournalBizTypeWithdrawalRequest
	JournalBizTypeWithdrawalComplete
	JournalBizTypeWithdrawalReject
	JournalBizTypeWithdrawalCancel
	JournalBizTypeTransfer
	JournalBizTypeCheckoutPayment
	JournalBizTypeCheckoutPaymentHold
	JournalBizTypeCheckoutPaymentHoldRelease
)

var _journalBizTypeToString = map[JournalBizType]string{
	JournalBizTypeDeposit:                    "deposit",
	JournalBizTypeWithdrawalRequest:          "withdrawal_request",
	JournalBizTypeWithdrawalComplete:         "withdrawal_complete",
	JournalBizTypeWithdrawalReject:           "withdrawal_reject",
	JournalBizTypeWithdrawalCancel:           "withdrawal_cancel",
	JournalBizTypeTransfer:                   "transfer",
	JournalBizTypeCheckoutPayment:            "checkout_payment",
	JournalBizTypeCheckoutPaymentHold:        "checkout_payment_hold",
	JournalBizTypeCheckoutPaymentHoldRelease: "checkout_payment_hold_release",
}

// String returns the string representation of the JournalBizType.
func (j JournalBizType) String() string {
	if s, ok := _journalBizTypeToString[j]; ok {
		return s
	}
	return "unknown"
}

type MakeTransactionParams struct {
	UserID         uint64
	Type           JournalBizType
	ToUserID       *uint64
	AssetCode      currency.AssetCode
	Amount         decimal.Decimal
	Fee            decimal.Decimal
	IdempotencyKey *string
	Description    *string
}

type MakeTransactionResponse struct {
	JournalID uint64
	Status    JournalStatus
}

// GetJournalByIdempotencyKeyParams identifies a journal entry by the same
// idempotency key that was passed to MakeTransaction.
//
// Type is required: account-service derives the stored key from the business type
// and the caller-supplied key together, so the same key under a different type is
// a different journal.
type GetJournalByIdempotencyKeyParams struct {
	Type           JournalBizType
	IdempotencyKey string
}

// GetJournalByIdempotencyKeyResult reports whether a live journal exists.
//
// Found is false both when the operation never happened and when it was already
// rolled back — a rollback renames the original key so the business operation can
// be re-attempted. For a caller asking whether there is a posting to compensate,
// both answers mean the same thing: there is not.
type GetJournalByIdempotencyKeyResult struct {
	Found     bool
	JournalID uint64
	Status    JournalStatus
}
