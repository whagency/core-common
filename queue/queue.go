package queue

import (
	"context"
	"errors"
)

var (
	PublishFailedErr = errors.New("queue publish failed")
	NonRetryableErr  = errors.New("queue non-retryable error")
)

const MessageTypeHeader = "MESSAGE_TYPE"

type PublisherType uint8

type ConnectionType uint8

type ConnectionConfig struct {
	Host     string
	VHost    string
	Port     string
	User     string
	Password string
}

// NonRetryableError marks an error as non-retryable for queue consumers.
type NonRetryableError struct {
	Err error
}

// Error returns the error message of the NonRetryableError. If the wrapped error is nil, it returns a default non-retryable error message.
func (e NonRetryableError) Error() string {
	if e.Err == nil {
		return NonRetryableErr.Error()
	}

	return e.Err.Error()
}

// Unwrap returns the wrapped error, allowing errors.Is and errors.As to work with NonRetryableError.
func (e NonRetryableError) Unwrap() error {
	return e.Err
}

// Is allows errors.Is to identify NonRetryableError as NonRetryableErr, enabling consumers to check for non-retryable errors using errors.Is.
func (e NonRetryableError) Is(target error) bool {
	return target == NonRetryableErr
}

// MarkNonRetryable wraps err with non-retryable marker used by consumers.
func MarkNonRetryable(err error) error {
	if err == nil {
		return nil
	}

	return NonRetryableError{Err: err}
}

// MessageType represents the type of the message.
type MessageType string

func NewMessageType(s string) MessageType {
	return MessageType(s)
}

// String returns the string representation of the MessageType.
func (m MessageType) String() string {
	return string(m)
}

// Message represents a message to be sent to the queue.
type Message struct {
	RoutingKeys []string
	MessageType MessageType
	Data        []byte
}

// String returns the string representation of the QueueMessage data.
func (q Message) String() string {
	return string(q.Data)
}

// Consumer defines the interface for a message consumer.
type Consumer interface {
	Name() string
	Run() error
	Close() error
}

// Publisher defines the interface for a message publisher.
type Publisher interface {
	Publish(context.Context, Message) error
	PublisherType() PublisherType
	ExchangeName() string
	Close() error
}

// ConsumerHandler defines the interface for handling consumed messages
type ConsumerHandler interface {
	Handle(context.Context, MessageType, []byte) error
}
