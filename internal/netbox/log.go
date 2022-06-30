package netbox

import (
	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"go.uber.org/zap"
)

// retryableHTTPLogger is a wrapper for zap logger that implements retyablehttp.LeveledLogger
// interface and therefore can be passed to a retryablehttp client
type retryableHTTPLogger struct {
	logger *zap.Logger
}

func newRetryableHTTPLogger(logger *zap.Logger) retryablehttp.LeveledLogger {
	return &retryableHTTPLogger{logger: logger}
}

func (l *retryableHTTPLogger) Error(msg string, keysAndValues ...interface{}) {
	l.logger.Error(msg, fieldsFromKeysAndValues(keysAndValues)...)
}

func (l *retryableHTTPLogger) Info(msg string, keysAndValues ...interface{}) {
	l.logger.Info(msg, fieldsFromKeysAndValues(keysAndValues)...)
}

func (l *retryableHTTPLogger) Debug(msg string, keysAndValues ...interface{}) {
	l.logger.Info(msg, fieldsFromKeysAndValues(keysAndValues)...)
}

func (l *retryableHTTPLogger) Warn(msg string, keysAndValues ...interface{}) {
	l.logger.Info(msg, fieldsFromKeysAndValues(keysAndValues)...)
}

func fieldsFromKeysAndValues(keysAndValues []interface{}) []zap.Field {
	var fields []zap.Field
	for i := 1; i < len(keysAndValues); i += 2 {
		key := keysAndValues[i-1]
		value := keysAndValues[i]
		if keyStr, ok := key.(string); ok {
			fields = append(fields, zap.Any(keyStr, value))
		}
		// ignore malformed key-value pair
	}
	return fields
}
