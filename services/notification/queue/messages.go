package queue

import commonQueue "github.com/whagency/core-common/queue"

const (
	MessageTypeOTP  commonQueue.MessageType = "notification.otp"
	MessageTypePush commonQueue.MessageType = "notification.push"
)

// OTPMessage is the shared queue payload for one-time password notifications.
type OTPMessage struct {
	Purpose      string `json:"purpose"`
	ContactType  string `json:"contact_type"`
	ContactValue string `json:"contact_value"`
	Code         string `json:"code"`
	Locale       string `json:"locale"`
}

// PushMessage is the shared queue payload for push notifications.
type PushMessage struct {
	Purpose string         `json:"purpose"`
	Data    map[string]any `json:"data"`
	Locale  string         `json:"locale"`

	// DedupeKey is an optional stable identity for this notification. Producers
	// that deliver at-least-once — anything publishing from a transactional
	// outbox — should set it so a retried publish does not reach the user twice.
	//
	// Empty means no deduplication, which is the historical behaviour and stays
	// valid for producers that publish exactly once.
	DedupeKey string `json:"dedupe_key,omitempty"`
}
