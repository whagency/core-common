package queue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/whagency/core-common/logger"
)

type testConsumer struct {
	name    string
	runFn   func() error
	closeFn func() error
}

var initTestLoggerOnce sync.Once

func initTestLogger(t *testing.T) {
	t.Helper()

	initTestLoggerOnce.Do(func() {
		err := logger.Init(logger.Config{
			AppName:     "queue-tests",
			Environment: "test",
			Level:       "debug",
		})
		assert.NoError(t, err)
	})
}

func (c *testConsumer) Name() string {
	return c.name
}

func (c *testConsumer) Run() error {
	if c.runFn == nil {
		return nil
	}
	return c.runFn()
}

func (c *testConsumer) Close() error {
	if c.closeFn == nil {
		return nil
	}
	return c.closeFn()
}

func TestRunConsumers_NoConsumers(t *testing.T) {
	initTestLogger(t)

	manager := NewManager()

	err := manager.RunConsumers()
	assert.NoError(t, err)
}

func TestRunConsumers_FailFastStopsOtherConsumers(t *testing.T) {
	initTestLogger(t)

	manager := NewManager()

	stop := make(chan struct{})
	closeCalled := make(chan struct{}, 1)
	var closeOnce sync.Once

	blockingConsumer := &testConsumer{
		name: "blocking-consumer",
		runFn: func() error {
			<-stop
			return nil
		},
		closeFn: func() error {
			closeOnce.Do(func() {
				close(stop)
				closeCalled <- struct{}{}
			})
			return nil
		},
	}

	failingConsumer := &testConsumer{
		name: "failing-consumer",
		runFn: func() error {
			return errors.New("boom")
		},
	}

	assert.NoError(t, manager.AddConsumer(blockingConsumer))
	assert.NoError(t, manager.AddConsumer(failingConsumer))

	started := time.Now()
	err := manager.RunConsumers()
	duration := time.Since(started)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failing-consumer")
	assert.Contains(t, err.Error(), "boom")
	assert.Less(t, duration, 2*time.Second)

	select {
	case <-closeCalled:
	default:
		t.Fatal("expected blocking consumer to be closed after first failure")
	}
}

func TestRunConsumers_ShutdownTimeout(t *testing.T) {
	initTestLogger(t)

	manager := NewManager()
	manager.runConsumersShutdownTimeout = 50 * time.Millisecond

	stop := make(chan struct{})

	stuckConsumer := &testConsumer{
		name: "stuck-consumer",
		runFn: func() error {
			<-stop
			return nil
		},
		// Simulate a broken Close implementation that does not stop Run().
		closeFn: func() error {
			return nil
		},
	}

	failingConsumer := &testConsumer{
		name: "failing-consumer",
		runFn: func() error {
			return errors.New("boom")
		},
	}

	assert.NoError(t, manager.AddConsumer(stuckConsumer))
	assert.NoError(t, manager.AddConsumer(failingConsumer))

	started := time.Now()
	err := manager.RunConsumers()
	elapsed := time.Since(started)
	close(stop) // cleanup to release goroutine after assertion path

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
	assert.Contains(t, err.Error(), "timeout waiting consumers shutdown")
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond)
	assert.Less(t, elapsed, 2*time.Second)
}

func TestCloseConsumers_DoesNotBlockWhileRunConsumersActive(t *testing.T) {
	initTestLogger(t)

	manager := NewManager()

	stop := make(chan struct{})
	blockingStarted := make(chan struct{})

	blockingConsumer := &testConsumer{
		name: "blocking-consumer",
		runFn: func() error {
			close(blockingStarted)
			<-stop
			return nil
		},
		closeFn: func() error {
			select {
			case <-stop:
			default:
				close(stop)
			}
			return nil
		},
	}

	assert.NoError(t, manager.AddConsumer(blockingConsumer))

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- manager.RunConsumers()
	}()

	<-blockingStarted

	closeErrCh := make(chan error, 1)
	go func() {
		closeErrCh <- manager.CloseConsumers()
	}()

	select {
	case closeErr := <-closeErrCh:
		assert.NoError(t, closeErr)
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected CloseConsumers to complete while RunConsumers is active")
	}

	select {
	case runErr := <-runErrCh:
		assert.NoError(t, runErr)
	case <-time.After(time.Second):
		t.Fatal("RunConsumers did not stop after CloseConsumers")
	}
}

// testPublisher is a minimal Publisher implementation for tests.
type testPublisher struct {
	pType        PublisherType
	exchangeName string
	closeCalled  bool
}

func (p *testPublisher) Publish(_ context.Context, _ Message) error { return nil }
func (p *testPublisher) PublisherType() PublisherType               { return p.pType }
func (p *testPublisher) ExchangeName() string                       { return p.exchangeName }
func (p *testPublisher) Close() error {
	p.closeCalled = true
	return nil
}

func TestAddConsumer_DuplicateReturnsError(t *testing.T) {
	initTestLogger(t)

	manager := NewManager()

	c := &testConsumer{name: "my-consumer"}
	assert.NoError(t, manager.AddConsumer(c))
	err := manager.AddConsumer(c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "my-consumer")
	assert.Contains(t, err.Error(), "already exists")
}

func TestAddPublisher_And_GetPublisher(t *testing.T) {
	initTestLogger(t)

	manager := NewManager()

	pub := &testPublisher{pType: PublisherType(1), exchangeName: "test-exchange"}
	assert.NoError(t, manager.AddPublisher(pub))

	got, err := manager.GetPublisher(PublisherType(1))
	assert.NoError(t, err)
	assert.Equal(t, pub, got)
}

func TestAddPublisher_DuplicateReturnsError(t *testing.T) {
	initTestLogger(t)

	manager := NewManager()

	pub := &testPublisher{pType: PublisherType(2)}
	assert.NoError(t, manager.AddPublisher(pub))
	err := manager.AddPublisher(pub)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestGetPublisher_NotFoundReturnsError(t *testing.T) {
	initTestLogger(t)

	manager := NewManager()

	_, err := manager.GetPublisher(PublisherType(99))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestClosePublishers_CallsCloseOnAll(t *testing.T) {
	initTestLogger(t)

	manager := NewManager()

	pub1 := &testPublisher{pType: PublisherType(1)}
	pub2 := &testPublisher{pType: PublisherType(2)}
	assert.NoError(t, manager.AddPublisher(pub1))
	assert.NoError(t, manager.AddPublisher(pub2))

	assert.NoError(t, manager.ClosePublishers())
	assert.True(t, pub1.closeCalled)
	assert.True(t, pub2.closeCalled)
}
