package queue

import (
	"time"

	commonQueue "github.com/whagency/core-common/queue"
)

const MessageTypeMerchantOperationSync commonQueue.MessageType = "pay.merchant_operation.sync"

// MerchantOperationSyncEvent carries a projection snapshot for merchant-service read model synchronization.
//
// Queue MessageType already identifies this event family.
type MerchantOperationSyncEvent struct {
	ProducedAt           time.Time  `json:"producedAt"`
	OrderID              string     `json:"orderId"`
	MerchantID           string     `json:"merchantId"`
	MerchantOrderID      *string    `json:"merchantOrderId,omitempty"`
	OperationType        uint8      `json:"operationType"`
	AssetCode            string     `json:"assetCode"`
	Amount               string     `json:"amount"`
	FeeAmount            string     `json:"feeAmount"`
	Status               string     `json:"status"`
	OccurredAt           *time.Time `json:"occurredAt,omitempty"`
	SettlementStatus     *string    `json:"settlementStatus,omitempty"`
	SettlementReleaseAt  *time.Time `json:"settlementReleaseAt,omitempty"`
	SettlementReleasedAt *time.Time `json:"settlementReleasedAt,omitempty"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}
