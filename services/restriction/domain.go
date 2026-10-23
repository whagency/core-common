package restriction

import (
	"fmt"
	"time"

	"github.com/whagency/core-common/services/currency"

	"github.com/shopspring/decimal"
)

// LimitOperationType represents the type of operation
type LimitOperationType uint8

const (
	LimitOperationTypeDeposit LimitOperationType = iota + 1
	LimitOperationTypeWithdrawal
	LimitOperationTypeSwap
	LimitOperationTypeTransfer
	LimitOperationTypeCheckoutPayment
)

// _limitOperationTypeToString maps LimitOperationType to their string representations
var _limitOperationTypeToString = map[LimitOperationType]string{
	LimitOperationTypeDeposit:         "deposit",
	LimitOperationTypeWithdrawal:      "withdrawal",
	LimitOperationTypeSwap:            "swap",
	LimitOperationTypeTransfer:        "transfer",
	LimitOperationTypeCheckoutPayment: "checkout_payment",
}

// Uint8 converts LimitOperationType to uint8
func (r LimitOperationType) Uint8() uint8 {
	return uint8(r)
}

// String returns the string representation of the LimitOperationType
func (r LimitOperationType) String() string {
	if str, ok := _limitOperationTypeToString[r]; ok {
		return str
	}
	return "unknown"
}

type DecisionType string

const (
	DecisionTypeAllow                DecisionType = "allow"
	DecisionTypeAllowWith2FA         DecisionType = "allow_with_2fa"
	DecisionTypeDenyLimitExceeded    DecisionType = "deny_limit_exceeded"
	DecisionTypeDenyAccount          DecisionType = "deny_account"
	DecisionTypeDenyGeo              DecisionType = "deny_geo"
	DecisionTypeRequiredVerification DecisionType = "required_verification"
)

var _decisionTypeToValue = map[string]DecisionType{
	"allow":                 DecisionTypeAllow,
	"allow_with_2fa":        DecisionTypeAllowWith2FA,
	"deny_limit_exceeded":   DecisionTypeDenyLimitExceeded,
	"deny_account":          DecisionTypeDenyAccount,
	"deny_geo":              DecisionTypeDenyGeo,
	"required_verification": DecisionTypeRequiredVerification,
}

var _decisionTypeToString = map[DecisionType]string{
	DecisionTypeAllow:                "allow",
	DecisionTypeAllowWith2FA:         "allow_with_2fa",
	DecisionTypeDenyLimitExceeded:    "deny_limit_exceeded",
	DecisionTypeDenyAccount:          "deny_account",
	DecisionTypeDenyGeo:              "deny_geo",
	DecisionTypeRequiredVerification: "required_verification",
}

// NewDecisionTypeFromString returns DecisionType of the given string representation.
func NewDecisionTypeFromString(s string) (DecisionType, error) {
	if val, ok := _decisionTypeToValue[s]; ok {
		return val, nil
	}
	return "", fmt.Errorf("[NewDecisionTypeFromString][err: unknown decision type: %s]", s)
}

// String returns the string representation of the DecisionType.
func (d DecisionType) String() string {
	if str, ok := _decisionTypeToString[d]; ok {
		return str
	}
	return "unknown"
}

type GeoFilterOperationType uint8

const (
	GeoFilterOperationTypeRegistration GeoFilterOperationType = iota + 1
	GeoFilterOperationTypeDeposit
	GeoFilterOperationTypeWithdrawal
	GeoFilterOperationTypeSwap
	GeoFilterOperationTypeTransfer
	GeoFilterOperationTypeCheckoutPayment
)

// Uint8 converts GeoFilterOperationType to uint8
func (r GeoFilterOperationType) Uint8() uint8 {
	return uint8(r)
}

type CounterUpdateParams struct {
	UserID         uint64
	OperationType  LimitOperationType
	Amount         decimal.Decimal
	AssetCode      currency.AssetCode
	OccurredAt     time.Time
	IsReverse      bool
	IdempotencyKey *string // nil = no idempotency guard
}

type CheckLimitParams struct {
	UserID        uint64
	OperationType LimitOperationType
	Amount        decimal.Decimal
	AssetCode     currency.AssetCode
	Country       string
}

type CheckLimitResult struct {
	OperationType LimitOperationType
	Decision      DecisionType
	Reason        *string
}

// IsAllowed checks if the decision allows the operation.
func (r CheckLimitResult) IsAllowed() bool {
	return r.Decision == DecisionTypeAllow || r.Decision == DecisionTypeAllowWith2FA
}

// GetErrorResponseData prepares the data for error responses.
func (r CheckLimitResult) GetErrorResponseData() map[string]any {
	data := make(map[string]any)
	data["operation_type"] = r.OperationType.String()
	data["restriction_decision"] = r.Decision
	if r.Reason != nil {
		data["restriction_reason"] = *r.Reason
	}
	return data
}
