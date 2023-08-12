package infrastructure

import (
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"testing"
)

func Test_initializeLog(t *testing.T) {
	initializeLog()
	assert.NotNil(t, log)
	_, ok := log.Formatter.(*logrus.JSONFormatter)
	assert.True(t, ok, "Expected JSONFormatter")
}

func TestGetLog(t *testing.T) {
	// Ensuring log is nil to test initialization
	log = nil

	logger := GetLog()
	assert.NotNil(t, logger)
	_, ok := logger.Formatter.(*logrus.JSONFormatter)
	assert.True(t, ok, "Expected JSONFormatter")

	// Ensuring that the logger returned is a singleton
	logger2 := GetLog()
	assert.Equal(t, logger, logger2, "Expected the same logger instance")
}

func TestSetLog(t *testing.T) {
	SetLog(logrus.New())
	assert.NotNil(t, log)
}
