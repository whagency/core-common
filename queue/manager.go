package queue

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/whagency/core-common/logger"
)

const _defaultRunConsumersShutdownTimeout = 5 * time.Second

// Manager defines the structure of the queue manager
type Manager struct {
	consumers  map[string]Consumer
	publishers map[PublisherType]Publisher
	mu         sync.RWMutex

	lifecycleMu                 sync.Mutex
	runConsumersShutdownTimeout time.Duration
}

// NewManager creates a new instance of Manager
func NewManager() *Manager {
	return &Manager{
		consumers:                   make(map[string]Consumer),
		publishers:                  make(map[PublisherType]Publisher),
		runConsumersShutdownTimeout: _defaultRunConsumersShutdownTimeout,
	}
}

// AddConsumer adds a new consumer to the manager
func (c *Manager) AddConsumer(consumer Consumer) error {
	logger.Log().Debugf("[queue][Manager:AddConsumer][consumerName: %s]", consumer.Name())

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if the consumer already exists
	if _, ok := c.consumers[consumer.Name()]; ok {
		return fmt.Errorf("[queue][Manager:AddConsumer][err: consumer %s already exists]", consumer.Name())
	}

	c.consumers[consumer.Name()] = consumer

	return nil
}

// AddPublisher adds a new publisher to the manager
func (c *Manager) AddPublisher(publisher Publisher) error {
	logger.Log().Debugf("[queue][Manager:AddPublisher][publisherType: %d]", publisher.PublisherType())

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if the publisher already exists
	if _, ok := c.publishers[publisher.PublisherType()]; ok {
		return fmt.Errorf("[queue][Manager:AddPublisher][err: publisher %d already exists]", publisher.PublisherType())
	}

	c.publishers[publisher.PublisherType()] = publisher

	return nil
}

// GetPublisher returns a publisher by its type
func (c *Manager) GetPublisher(publisherType PublisherType) (Publisher, error) {
	logger.Log().Debugf("[queue][Manager:GetPublisher][publisherType: %d]", publisherType)

	c.mu.RLock()
	defer c.mu.RUnlock()

	publisher, ok := c.publishers[publisherType]
	if !ok {
		return nil, fmt.Errorf("[queue][Manager:GetPublisher][err: publisher %d not found]", publisherType)
	}

	return publisher, nil
}

// RunConsumers runs the consumers in different goroutines
func (c *Manager) RunConsumers() error {
	logger.Log().Debug("[queue][Manager:RunConsumers]")

	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	consumers := c.getConsumersSnapshot()
	if len(consumers) == 0 {
		return nil
	}

	var stopOnce sync.Once
	stopOthers := func(failedConsumerName string) {
		stopOnce.Do(func() {
			for _, consumer := range consumers {
				if consumer == nil {
					continue
				}
				if failedConsumerName != "" && consumer.Name() == failedConsumerName {
					continue
				}
				if err := consumer.Close(); err != nil {
					logger.Log().Warnf(
						"[queue][Manager:RunConsumers][err: closing consumer %s after failure: %s]",
						consumer.Name(),
						err,
					)
				}
			}
		})
	}

	errorChan := make(chan error, 1)
	var wg sync.WaitGroup
	for _, consumer := range consumers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := consumer.Run(); err != nil {
				runErr := fmt.Errorf("[queue][Manager:RunConsumers][consumerName:%s][err:%w]", consumer.Name(), err)
				stopOthers(consumer.Name())
				select {
				case errorChan <- runErr:
				default:
				}
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case err := <-errorChan:
		// Ensure we attempt to stop any remaining consumers even if run returned early.
		stopOthers("")
		if shutdownErr := waitDoneWithTimeout(done, c.runConsumersShutdownTimeout); shutdownErr != nil {
			return fmt.Errorf("[queue][Manager:RunConsumers][err:%w]", errors.Join(err, shutdownErr))
		}
		return err
	case <-done:
		return nil
	}
}

// getConsumersSnapshot returns a snapshot of the current consumers to avoid holding the lock while running them
func (c *Manager) getConsumersSnapshot() []Consumer {
	c.mu.RLock()
	defer c.mu.RUnlock()

	consumers := make([]Consumer, 0, len(c.consumers))
	for _, consumer := range c.consumers {
		consumers = append(consumers, consumer)
	}

	return consumers
}

// CloseConsumers closes the consumers
func (c *Manager) CloseConsumers() error {
	logger.Log().Debug("[queue][Manager:CloseConsumers]")

	consumers := c.getConsumersSnapshot()
	if len(consumers) == 0 {
		return nil
	}

	for _, consumer := range consumers {
		if err := consumer.Close(); err != nil {
			logger.Log().Warnf("[queue][Manager:CloseConsumers][err: closing consumer %s: %s]", consumer.Name(), err)
		}
	}

	return nil
}

// waitDoneWithTimeout waits for the done channel to be closed with a timeout. If the timeout is reached before the done channel is closed, an error is returned.
func waitDoneWithTimeout(done <-chan struct{}, timeout time.Duration) error {
	if timeout <= 0 {
		<-done
		return nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-done:
		return nil
	case <-timer.C:
		return fmt.Errorf("[queue][waitDoneWithTimeout][err: timeout waiting consumers shutdown after %s]", timeout)
	}
}

// getPublishersSnapshot returns a snapshot of the current publishers to avoid holding the lock while closing them
func (c *Manager) getPublishersSnapshot() []Publisher {
	c.mu.RLock()
	defer c.mu.RUnlock()

	publishers := make([]Publisher, 0, len(c.publishers))
	for _, publisher := range c.publishers {
		publishers = append(publishers, publisher)
	}

	return publishers
}

// ClosePublishers closes the publishers
func (c *Manager) ClosePublishers() error {
	logger.Log().Debug("[queue][Manager:ClosePublishers]")

	for _, publisher := range c.getPublishersSnapshot() {
		if err := publisher.Close(); err != nil {
			logger.Log().Warnf("[queue][Manager:ClosePublishers][err: closing publisher %d: %s]", publisher.PublisherType(), err)
		}
	}

	return nil
}
