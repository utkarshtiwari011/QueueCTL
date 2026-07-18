package logger

import (
	"context"
	"fmt"
	"strings"
	"time"

	"queuectl/internal/config"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ContextKey defines a custom type for context key mapping.
type ContextKey string

const (
	RequestIDKey ContextKey = "request_id"
	WorkerIDKey  ContextKey = "worker_id"
)

// Field represents a structured logging field to abstract underlying Zap types.
type Field struct {
	Key   string
	Value any
}

// Logger defines the application-wide logging contract.
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)
	With(fields ...Field) Logger
	WithContext(ctx context.Context) Logger
	Sync() error
}

type zapLogger struct {
	zap *zap.Logger
}

// New creates and configures a production-grade structured Logger wrapper.
func New(cfg config.LoggerConfig) (Logger, error) {
	var zapConfig zap.Config
	var encoderConfig zapcore.EncoderConfig

	if strings.ToLower(cfg.Format) == "json" {
		encoderConfig = zap.NewProductionEncoderConfig()
		zapConfig = zap.NewProductionConfig()
	} else {
		encoderConfig = zap.NewDevelopmentEncoderConfig()
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder // Colored console levels
		zapConfig = zap.NewDevelopmentConfig()
	}

	// Sane structured formatting overrides
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.MessageKey = "message"
	encoderConfig.LevelKey = "level"
	encoderConfig.CallerKey = "caller"
	encoderConfig.StacktraceKey = "stacktrace"
	zapConfig.EncoderConfig = encoderConfig

	// Disable standard caller/stacktrace for cleaner manual fields unless error level is used
	zapConfig.Development = false

	var level zapcore.Level
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = zap.DebugLevel
	case "info":
		level = zap.InfoLevel
	case "warn":
		level = zap.WarnLevel
	case "error":
		level = zap.ErrorLevel
	default:
		level = zap.InfoLevel
	}
	zapConfig.Level = zap.NewAtomicLevelAt(level)

	z, err := zapConfig.Build(zap.AddStacktrace(zap.ErrorLevel))
	if err != nil {
		return nil, fmt.Errorf("failed to build zap logger: %w", err)
	}

	return &zapLogger{zap: z}, nil
}

// Field constructors for convenient parameter passing
func String(key, val string) Field                 { return Field{Key: key, Value: val} }
func Int(key string, val int) Field                { return Field{Key: key, Value: val} }
func Int64(key string, val int64) Field            { return Field{Key: key, Value: val} }
func Duration(key string, val time.Duration) Field { return Field{Key: key, Value: val} }
func Any(key string, val any) Field                { return Field{Key: key, Value: val} }

// Specific standard field constructors
func RequestID(val string) Field { return Field{Key: "request_id", Value: val} }
func WorkerID(val string) Field  { return Field{Key: "worker_id", Value: val} }
func ExecutionTime(val time.Duration) Field {
	return Field{Key: "execution_time_ms", Value: val.Milliseconds()}
}
func Error(err error) Field {
	if err == nil {
		return Field{Key: "error", Value: nil}
	}
	return Field{Key: "error", Value: err.Error()}
}

func (l *zapLogger) toZapFields(fields []Field) []zap.Field {
	zf := make([]zap.Field, len(fields))
	for i, f := range fields {
		zf[i] = zap.Any(f.Key, f.Value)
	}
	return zf
}

func (l *zapLogger) Debug(msg string, fields ...Field) {
	l.zap.Debug(msg, l.toZapFields(fields)...)
}

func (l *zapLogger) Info(msg string, fields ...Field) {
	l.zap.Info(msg, l.toZapFields(fields)...)
}

func (l *zapLogger) Warn(msg string, fields ...Field) {
	l.zap.Warn(msg, l.toZapFields(fields)...)
}

func (l *zapLogger) Error(msg string, fields ...Field) {
	l.zap.Error(msg, l.toZapFields(fields)...)
}

func (l *zapLogger) Fatal(msg string, fields ...Field) {
	l.zap.Fatal(msg, l.toZapFields(fields)...)
}

func (l *zapLogger) With(fields ...Field) Logger {
	return &zapLogger{zap: l.zap.With(l.toZapFields(fields)...)}
}

// WithContext inspects the context for trace keys (request_id, worker_id) and appends them.
func (l *zapLogger) WithContext(ctx context.Context) Logger {
	if ctx == nil {
		return l
	}

	var fields []Field

	if reqID, ok := ctx.Value(RequestIDKey).(string); ok && reqID != "" {
		fields = append(fields, RequestID(reqID))
	}

	if wrkID, ok := ctx.Value(WorkerIDKey).(string); ok && wrkID != "" {
		fields = append(fields, WorkerID(wrkID))
	}

	if len(fields) > 0 {
		return l.With(fields...)
	}

	return l
}

func (l *zapLogger) Sync() error {
	return l.zap.Sync()
}

// NewNop returns a no-op logger that discards all logs. Useful in tests.
func NewNop() Logger {
	return &zapLogger{zap: zap.NewNop()}
}
