package infrastructure

import "github.com/sirupsen/logrus"

// log holds the singleton instance of the logger.
var log *logrus.Logger

// initializeLog sets up the logger with default configuration.
func initializeLog() {
	log = logrus.New()
	log.Formatter = &logrus.JSONFormatter{}
}

// GetLog provides access to the shared logger instance,
// initializing it if it has not been already.
func GetLog() *logrus.Logger {
	if log == nil {
		initializeLog()
	}

	return log
}

func SetLog(logger *logrus.Logger) {
	log = logger
}
