package grpc

import (
	"fmt"

	"github.com/whagency/core-common/grpc/pb/restrictionv1"
	"github.com/whagency/core-common/services/restriction"
)

// encodeOperationType converts a restriction.LimitOperationType to gRPC operation type.
func encodeOperationType(value restriction.LimitOperationType) (restrictionv1.OperationType, error) {
	switch value {
	case restriction.LimitOperationTypeDeposit:
		return restrictionv1.OperationType_OPERATION_TYPE_DEPOSIT, nil
	case restriction.LimitOperationTypeWithdrawal:
		return restrictionv1.OperationType_OPERATION_TYPE_WITHDRAWAL, nil
	case restriction.LimitOperationTypeSwap:
		return restrictionv1.OperationType_OPERATION_TYPE_SWAP, nil
	case restriction.LimitOperationTypeTransfer:
		return restrictionv1.OperationType_OPERATION_TYPE_TRANSFER, nil
	case restriction.LimitOperationTypeCheckoutPayment:
		return restrictionv1.OperationType_OPERATION_TYPE_CHECKOUT_PAYMENT, nil
	default:
		return 0, fmt.Errorf("[encodeOperationType][err: unsupported geo filter operation_type: %d]", value)
	}
}
