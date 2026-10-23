package rabbitmq

import "github.com/whagency/core-common/logger"

type QueueLogger struct{}

func (QueueLogger) Infof(s string, m ...interface{}) {
	logger.Log().Infof(s, m...)
}

func (QueueLogger) Warnf(s string, m ...interface{}) {
	logger.Log().Warnf(s, m...)
}

func (QueueLogger) Fatalf(s string, m ...interface{}) {
	logger.Log().Fatalf(s, m...)
}

func (QueueLogger) Errorf(s string, m ...interface{}) {
	logger.Log().Errorf(s, m...)
}

func (QueueLogger) Debugf(s string, m ...interface{}) {
	logger.Log().Debugf(s, m...)
}
