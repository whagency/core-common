package queue

import (
	"time"

	commonQueue "github.com/whagency/core-common/queue"
)

const (
	MessageTypeUserCreated            commonQueue.MessageType = "user.created"
	MessageTypeUserVerifyLevelUpdated commonQueue.MessageType = "user.verify_level.updated"
)

// UserCreatedEvent is the shared queue payload for user creation lifecycle consumers.
type UserCreatedEvent struct {
	UserID     uint64    `json:"userId"`
	OccurredAt time.Time `json:"occurredAt"`
}

// UserVerifyLevelUpdatedEvent is the shared queue payload for KYC verification-level updates.
type UserVerifyLevelUpdatedEvent struct {
	UserID      uint64    `json:"userId"`
	VerifyLevel uint8     `json:"verifyLevel"`
	OccurredAt  time.Time `json:"occurredAt"`
}
