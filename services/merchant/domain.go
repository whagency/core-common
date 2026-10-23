package merchant

import (
	"context"
	"errors"
)

const (
	InvalidAPIKeyErrorCode       = "invalid_api_key"
	MerchantDisabledErrorCode    = "merchant_disabled"
	InvalidSignatureErrorCode    = "invalid_signature"
	TimestampOutOfRangeErrorCode = "timestamp_out_of_range"
	InvalidIPErrorCode           = "invalid_ip"
	MerchantNotFoundErrorCode    = "merchant_not_found"
	WebhookConfigNotFoundCode    = "webhook_config_not_found"
)

var (
	ErrInvalidAPIKey         = errors.New(InvalidAPIKeyErrorCode)
	ErrMerchantDisabled      = errors.New(MerchantDisabledErrorCode)
	ErrInvalidSignature      = errors.New(InvalidSignatureErrorCode)
	ErrTimestampOutOfRange   = errors.New(TimestampOutOfRangeErrorCode)
	ErrInvalidIP             = errors.New(InvalidIPErrorCode)
	ErrMerchantNotFound      = errors.New(MerchantNotFoundErrorCode)
	ErrWebhookConfigNotFound = errors.New(WebhookConfigNotFoundCode)
)

type AuthResponseMode uint8

const (
	AuthResponseModeAuthOnly AuthResponseMode = iota + 1
	AuthResponseModeAuthWithProfile
)

// AuthenticateMerchantParams is input for merchant auth verification RPC.
type AuthenticateMerchantParams struct {
	APIPublicKey string
	TimestampMS  int64
	HTTPMethod   string
	HTTPPath     string
	RawBody      []byte
	Signature    string
	ClientIP     string
	ResponseMode AuthResponseMode
}

// AuthenticateMerchantResult is normalized output from merchant auth verification RPC.
type AuthenticateMerchantResult struct {
	MerchantID        string
	Mode              Mode
	IsEnabled         bool
	SuccessURLDefault *string
	PendingURLDefault *string
	FailURLDefault    *string
	Profile           *MerchantProfile
}

type OperationType uint8

const (
	OperationTypePayment OperationType = iota + 1
	OperationTypeRefund
	OperationTypePayout
	OperationTypeChargeback
)

func (t OperationType) Uint8() uint8 {
	return uint8(t)
}

type FeeType uint8

const (
	FeeTypePercent FeeType = iota + 1
	FeeTypeFixed
)

var feeTypeToString = map[FeeType]string{
	FeeTypePercent: "percent",
	FeeTypeFixed:   "fixed",
}

// String returns canonical fee type value.
func (t FeeType) String() string {
	if value, ok := feeTypeToString[t]; ok {
		return value
	}
	return "unknown"
}

// Uint8 converts FeeType to uint8.
func (t FeeType) Uint8() uint8 {
	return uint8(t)
}

type GetMerchantProfileParams struct {
	MerchantID string
}

type PrepareWebhookDeliveryParams struct {
	MerchantID string
	Mode       Mode
	WebhookID  string
	RawBody    []byte
}

type FeeRule struct {
	OperationType OperationType
	FeeType       FeeType
	FeeValue      string
}

// MerchantProfile describes reusable merchant data for payment/profile flows.
type MerchantProfile struct {
	MerchantID         string
	MerchantCode       string
	DisplayName        string
	MerchantType       string
	MerchantStatus     string
	MerchantURL        string
	OwnerUserID        uint64
	SettlementHoldDays uint16
	FeeRules           []FeeRule
}

// PreparedWebhookDelivery describes internal webhook delivery data prepared by merchant-service.
// merchant-service remains the owner of webhook config and secret handling, while pay-service
// reuses the returned url/timestamp/signature to execute the actual delivery and retries.
type PreparedWebhookDelivery struct {
	MerchantID string
	Mode       Mode
	IsEnabled  bool
	WebhookURL *string
	Timestamp  string
	Signature  *string
}

// Service defines merchant grpc contract used by internal services.
type Service interface {
	AuthenticateMerchant(context.Context, AuthenticateMerchantParams) (AuthenticateMerchantResult, error)
	GetMerchantProfile(context.Context, GetMerchantProfileParams) (MerchantProfile, error)
	PrepareWebhookDelivery(context.Context, PrepareWebhookDeliveryParams) (PreparedWebhookDelivery, error)
}
