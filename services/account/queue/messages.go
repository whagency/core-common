package queue

import commonQueue "github.com/whagency/core-common/queue"

const MessageTypeTransactionRollbackRequested commonQueue.MessageType = "transaction.rollback.requested"

type TransactionRollbackRequestedMessage struct {
	ID uint64 `json:"id"`
}
