package internal

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Logger provides structured logfmt logging with context
type Logger struct {
	serviceName string
	fields      map[string]interface{}
}

// NewLogger creates a new logger instance for a service
func NewLogger(serviceName string) *Logger {
	return &Logger{
		serviceName: serviceName,
		fields:      make(map[string]interface{}),
	}
}

// WithDeviceID adds device_id to the logger context
func (l *Logger) WithDeviceID(deviceID string) *Logger {
	return l.withField("device_id", deviceID)
}

// WithStream adds stream information to the logger
func (l *Logger) WithStream(streamName string, action string) *Logger {
	logger := l.withField("stream_name", streamName)
	return logger.withField("stream_action", action)
}

// WithField adds a custom field to the logger context
func (l *Logger) WithField(key string, value interface{}) *Logger {
	return l.withField(key, value)
}

// WithFields adds multiple fields to the logger context
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	newLogger := &Logger{
		serviceName: l.serviceName,
		fields:      make(map[string]interface{}),
	}
	// Copy existing fields
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}
	// Add new fields
	for k, v := range fields {
		newLogger.fields[k] = v
	}
	return newLogger
}

// withField creates a new logger instance with an additional field
func (l *Logger) withField(key string, value interface{}) *Logger {
	newLogger := &Logger{
		serviceName: l.serviceName,
		fields:      make(map[string]interface{}),
	}
	// Copy existing fields
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}
	// Add new field
	newLogger.fields[key] = value
	return newLogger
}

// formatLogfmtValue formats a value for logfmt output
func formatLogfmtValue(v interface{}) string {
	if v == nil {
		return "null"
	}

	switch val := v.(type) {
	case string:
		// Quote strings that contain spaces, special characters, or are empty
		if val == "" || strings.ContainsAny(val, " =\"\n\t") {
			return strconv.Quote(val)
		}
		return val
	case bool:
		return strconv.FormatBool(val)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case time.Duration:
		return val.String()
	case time.Time:
		return val.Format(time.RFC3339Nano)
	default:
		// For complex types, convert to string and quote if needed
		str := fmt.Sprintf("%v", val)
		if strings.ContainsAny(str, " =\"\n\t") {
			return strconv.Quote(str)
		}
		return str
	}
}

// log writes a structured logfmt log entry
func (l *Logger) log(level, message string, err error, duration time.Duration, additionalFields map[string]interface{}) {
	var parts []string

	// Always include timestamp, level, service, and message
	parts = append(parts, fmt.Sprintf("timestamp=%s", time.Now().UTC().Format(time.RFC3339Nano)))
	parts = append(parts, fmt.Sprintf("level=%s", level))
	parts = append(parts, fmt.Sprintf("service=%s", l.serviceName))
	parts = append(parts, fmt.Sprintf("message=%s", formatLogfmtValue(message)))

	// Extract common fields from context
	if deviceID, ok := l.fields["device_id"].(string); ok && deviceID != "" {
		parts = append(parts, fmt.Sprintf("device_id=%s", formatLogfmtValue(deviceID)))
	}
	if streamName, ok := l.fields["stream_name"].(string); ok && streamName != "" {
		parts = append(parts, fmt.Sprintf("stream_name=%s", formatLogfmtValue(streamName)))
	}
	if streamAction, ok := l.fields["stream_action"].(string); ok && streamAction != "" {
		parts = append(parts, fmt.Sprintf("stream_action=%s", formatLogfmtValue(streamAction)))
	}

	// Add error if present
	if err != nil {
		parts = append(parts, fmt.Sprintf("error=%s", formatLogfmtValue(err.Error())))
	}

	// Add duration if present
	if duration > 0 {
		parts = append(parts, fmt.Sprintf("duration=%s", formatLogfmtValue(duration.String())))
	}

	// Add other context fields (excluding already processed ones)
	for k, v := range l.fields {
		if k != "device_id" && k != "stream_name" && k != "stream_action" {
			parts = append(parts, fmt.Sprintf("%s=%s", k, formatLogfmtValue(v)))
		}
	}

	// Add additional fields
	for k, v := range additionalFields {
		parts = append(parts, fmt.Sprintf("%s=%s", k, formatLogfmtValue(v)))
	}

	// Write to stdout in logfmt format
	fmt.Fprintln(os.Stdout, strings.Join(parts, " "))
}

// Info logs an info level message
func (l *Logger) Info(message string) {
	l.log("info", message, nil, 0, nil)
}

// Infof logs an info level message with formatting
func (l *Logger) Infof(format string, args ...interface{}) {
	l.Info(fmt.Sprintf(format, args...))
}

// InfoWithFields logs an info level message with additional fields
func (l *Logger) InfoWithFields(message string, fields map[string]interface{}) {
	l.log("info", message, nil, 0, fields)
}

// Error logs an error level message
func (l *Logger) Error(message string, err error) {
	l.log("error", message, err, 0, nil)
}

// Errorf logs an error level message with formatting
func (l *Logger) Errorf(format string, args ...interface{}) {
	var err error
	if len(args) > 0 {
		if e, ok := args[len(args)-1].(error); ok {
			err = e
			args = args[:len(args)-1]
		}
	}
	message := fmt.Sprintf(format, args...)
	l.log("error", message, err, 0, nil)
}

// ErrorWithFields logs an error level message with additional fields
func (l *Logger) ErrorWithFields(message string, err error, fields map[string]interface{}) {
	l.log("error", message, err, 0, fields)
}

// Warn logs a warning level message
func (l *Logger) Warn(message string) {
	l.log("warn", message, nil, 0, nil)
}

// Warnf logs a warning level message with formatting
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.Warn(fmt.Sprintf(format, args...))
}

// WarnWithFields logs a warning level message with additional fields
func (l *Logger) WarnWithFields(message string, fields map[string]interface{}) {
	l.log("warn", message, nil, 0, fields)
}

// Debug logs a debug level message
func (l *Logger) Debug(message string) {
	l.log("debug", message, nil, 0, nil)
}

// Debugf logs a debug level message with formatting
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.Debug(fmt.Sprintf(format, args...))
}

// DebugWithFields logs a debug level message with additional fields
func (l *Logger) DebugWithFields(message string, fields map[string]interface{}) {
	l.log("debug", message, nil, 0, fields)
}

// WithDuration logs a message with duration information
func (l *Logger) WithDuration(duration time.Duration) *Logger {
	return l.withField("duration", duration.String())
}

// LogOperation logs the start/end of an operation with duration
func (l *Logger) LogOperation(operation string, fn func() error) error {
	start := time.Now()
	opLogger := l.withField("operation", operation)
	opLogger.Info(fmt.Sprintf("Operation %s started", strings.ReplaceAll(operation, "_", " ")))

	err := fn()
	duration := time.Since(start)

	if err != nil {
		opLogger.ErrorWithFields(fmt.Sprintf("Operation %s failed", strings.ReplaceAll(operation, "_", " ")), err, map[string]interface{}{
			"duration": duration.String(),
		})
	} else {
		opLogger.InfoWithFields(fmt.Sprintf("Operation %s completed", strings.ReplaceAll(operation, "_", " ")), map[string]interface{}{
			"duration": duration.String(),
		})
	}

	return err
}

// Fatal logs a fatal error and exits
func (l *Logger) Fatal(message string, err error) {
	l.log("fatal", message, err, 0, nil)
	os.Exit(1)
}

// Fatalf logs a fatal error with formatting and exits
func (l *Logger) Fatalf(format string, args ...interface{}) {
	var err error
	if len(args) > 0 {
		if e, ok := args[len(args)-1].(error); ok {
			err = e
			args = args[:len(args)-1]
		}
	}
	message := fmt.Sprintf(format, args...)
	l.Fatal(message, err)
}
