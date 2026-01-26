package logging

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ContextKey is a type for context keys
type ContextKey string

const (
	// CorrelationIDKey is the context key for correlation ID
	CorrelationIDKey ContextKey = "correlation_id"
	// UserIDKey is the context key for user ID
	UserIDKey ContextKey = "user_id"
	// TenantIDKey is the context key for tenant ID
	TenantIDKey ContextKey = "tenant_id"
)

// LogLevel represents the severity of a log entry
type LogLevel string

const (
	// DebugLevel for debug messages
	DebugLevel LogLevel = "DEBUG"
	// InfoLevel for informational messages
	InfoLevel LogLevel = "INFO"
	// WarnLevel for warning messages
	WarnLevel LogLevel = "WARN"
	// ErrorLevel for error messages
	ErrorLevel LogLevel = "ERROR"
	// SecurityLevel for security events
	SecurityLevel LogLevel = "SECURITY"
)

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp     time.Time              `json:"timestamp"`
	Level         LogLevel               `json:"level"`
	Message       string                 `json:"message"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	UserID        string                 `json:"user_id,omitempty"`
	TenantID      string                 `json:"tenant_id,omitempty"`
	Service       string                 `json:"service,omitempty"`
	Fields        map[string]interface{} `json:"fields,omitempty"`
	Error         string                 `json:"error,omitempty"`
}

// Logger provides structured logging with correlation tracking
type Logger struct {
	service string
	writer  LogWriter
}

// LogWriter is an interface for writing log entries
type LogWriter interface {
	Write(entry LogEntry)
}

// ConsoleWriter writes logs to console in JSON format
type ConsoleWriter struct{}

// Write writes a log entry to console
func (w *ConsoleWriter) Write(entry LogEntry) {
	// In a real implementation, this would use a proper JSON encoder
	fmt.Printf(`{"timestamp":"%s","level":"%s","message":"%s","correlation_id":"%s","user_id":"%s","tenant_id":"%s","service":"%s"`,
		entry.Timestamp.Format(time.RFC3339),
		entry.Level,
		entry.Message,
		entry.CorrelationID,
		entry.UserID,
		entry.TenantID,
		entry.Service,
	)

	if len(entry.Fields) > 0 {
		fmt.Print(`,"fields":{`)
		first := true
		for k, v := range entry.Fields {
			if !first {
				fmt.Print(",")
			}
			fmt.Printf(`"%s":"%v"`, k, v)
			first = false
		}
		fmt.Print("}")
	}

	if entry.Error != "" {
		fmt.Printf(`,"error":"%s"`, entry.Error)
	}

	fmt.Println("}")
}

// NewLogger creates a new logger
func NewLogger(service string, writer LogWriter) *Logger {
	if writer == nil {
		writer = &ConsoleWriter{}
	}
	return &Logger{
		service: service,
		writer:  writer,
	}
}

// WithContext creates a log entry with context values
func (l *Logger) WithContext(ctx context.Context) *LogEntryBuilder {
	builder := &LogEntryBuilder{
		logger: l,
		entry: LogEntry{
			Timestamp: time.Now(),
			Service:   l.service,
			Fields:    make(map[string]interface{}),
		},
	}

	// Extract correlation ID from context
	if correlationID, ok := ctx.Value(CorrelationIDKey).(string); ok {
		builder.entry.CorrelationID = correlationID
	}

	// Extract user ID from context
	if userID, ok := ctx.Value(UserIDKey).(string); ok {
		builder.entry.UserID = userID
	}

	// Extract tenant ID from context
	if tenantID, ok := ctx.Value(TenantIDKey).(string); ok {
		builder.entry.TenantID = tenantID
	}

	return builder
}

// Debug logs a debug message
func (l *Logger) Debug(ctx context.Context, message string) {
	l.WithContext(ctx).Debug(message)
}

// Info logs an info message
func (l *Logger) Info(ctx context.Context, message string) {
	l.WithContext(ctx).Info(message)
}

// Warn logs a warning message
func (l *Logger) Warn(ctx context.Context, message string) {
	l.WithContext(ctx).Warn(message)
}

// Error logs an error message
func (l *Logger) Error(ctx context.Context, message string, err error) {
	l.WithContext(ctx).Error(message, err)
}

// Security logs a security event
func (l *Logger) Security(ctx context.Context, message string) {
	l.WithContext(ctx).Security(message)
}

// LogEntryBuilder provides a fluent interface for building log entries
type LogEntryBuilder struct {
	logger *Logger
	entry  LogEntry
}

// WithField adds a field to the log entry
func (b *LogEntryBuilder) WithField(key string, value interface{}) *LogEntryBuilder {
	b.entry.Fields[key] = value
	return b
}

// WithFields adds multiple fields to the log entry
func (b *LogEntryBuilder) WithFields(fields map[string]interface{}) *LogEntryBuilder {
	for k, v := range fields {
		b.entry.Fields[k] = v
	}
	return b
}

// WithError adds an error to the log entry
func (b *LogEntryBuilder) WithError(err error) *LogEntryBuilder {
	if err != nil {
		b.entry.Error = err.Error()
	}
	return b
}

// Debug logs at debug level
func (b *LogEntryBuilder) Debug(message string) {
	b.entry.Level = DebugLevel
	b.entry.Message = message
	b.logger.writer.Write(b.entry)
}

// Info logs at info level
func (b *LogEntryBuilder) Info(message string) {
	b.entry.Level = InfoLevel
	b.entry.Message = message
	b.logger.writer.Write(b.entry)
}

// Warn logs at warn level
func (b *LogEntryBuilder) Warn(message string) {
	b.entry.Level = WarnLevel
	b.entry.Message = message
	b.logger.writer.Write(b.entry)
}

// Error logs at error level
func (b *LogEntryBuilder) Error(message string, err error) {
	b.entry.Level = ErrorLevel
	b.entry.Message = message
	if err != nil {
		b.entry.Error = err.Error()
	}
	b.logger.writer.Write(b.entry)
}

// Security logs a security event
func (b *LogEntryBuilder) Security(message string) {
	b.entry.Level = SecurityLevel
	b.entry.Message = message
	b.logger.writer.Write(b.entry)
}

// GenerateCorrelationID generates a new correlation ID
func GenerateCorrelationID() string {
	return uuid.New().String()
}

// WithCorrelationID adds a correlation ID to the context
func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, CorrelationIDKey, correlationID)
}

// GetCorrelationID retrieves the correlation ID from context
func GetCorrelationID(ctx context.Context) string {
	if correlationID, ok := ctx.Value(CorrelationIDKey).(string); ok {
		return correlationID
	}
	return ""
}

// WithUserID adds a user ID to the context
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, UserIDKey, userID)
}

// GetUserID retrieves the user ID from context
func GetUserID(ctx context.Context) string {
	if userID, ok := ctx.Value(UserIDKey).(string); ok {
		return userID
	}
	return ""
}

// WithTenantID adds a tenant ID to the context
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, TenantIDKey, tenantID)
}

// GetTenantID retrieves the tenant ID from context
func GetTenantID(ctx context.Context) string {
	if tenantID, ok := ctx.Value(TenantIDKey).(string); ok {
		return tenantID
	}
	return ""
}
