package logger

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// Level represents log severity levels
type Level int

const (
	DEBUG Level = iota
	VERBOSE
	INFO
	WARNING
	ERROR
)

var levelNames = map[Level]string{
	DEBUG:   "DEBUG",
	VERBOSE: "VERBOSE",
	INFO:    "INFO",
	WARNING: "WARNING",
	ERROR:   "ERROR",
}

// Logger provides leveled logging functionality
type Logger struct {
	level  Level
	logger *log.Logger
}

// New creates a new Logger with the specified level
func New(levelStr string) *Logger {
	level := ParseLevel(levelStr)
	return &Logger{
		level:  level,
		logger: log.New(os.Stdout, "", log.LstdFlags),
	}
}

// ParseLevel converts a string to a Level
func ParseLevel(levelStr string) Level {
	levelStr = strings.ToUpper(strings.TrimSpace(levelStr))
	switch levelStr {
	case "DEBUG":
		return DEBUG
	case "VERBOSE":
		return VERBOSE
	case "INFO":
		return INFO
	case "WARNING", "WARN":
		return WARNING
	case "ERROR":
		return ERROR
	default:
		return INFO // Default to INFO
	}
}

// GetLevel returns the current log level
func (l *Logger) GetLevel() Level {
	return l.level
}

// SetLevel sets the log level
func (l *Logger) SetLevel(level Level) {
	l.level = level
}

func (l *Logger) shouldLog(level Level) bool {
	return level >= l.level
}

func (l *Logger) log(level Level, format string, args ...interface{}) {
	if !l.shouldLog(level) {
		return
	}
	levelName := levelNames[level]
	prefix := fmt.Sprintf("[%s] ", levelName)
	message := fmt.Sprintf(format, args...)
	l.logger.Printf("%s%s", prefix, message)
}

func (l *Logger) logMessage(level Level, message string) {
	if !l.shouldLog(level) {
		return
	}
	levelName := levelNames[level]
	prefix := fmt.Sprintf("[%s] ", levelName)
	l.logger.Printf("%s%s", prefix, message)
}

// Debug logs a debug message
func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(DEBUG, format, args...)
}

// Verbose logs a verbose message
func (l *Logger) Verbose(format string, args ...interface{}) {
	l.log(VERBOSE, format, args...)
}

// Info logs an info message
func (l *Logger) Info(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// Warning logs a warning message
func (l *Logger) Warning(format string, args ...interface{}) {
	l.log(WARNING, format, args...)
}

// Error logs an error message
func (l *Logger) Error(format string, args ...interface{}) {
	l.log(ERROR, format, args...)
}

// WithFields creates a contextual logger with fields
func (l *Logger) WithFields(fields map[string]interface{}) *ContextLogger {
	return &ContextLogger{
		logger: l,
		fields: fields,
	}
}

// ContextLogger provides structured logging with fields
type ContextLogger struct {
	logger *Logger
	fields map[string]interface{}
}

func (cl *ContextLogger) formatMessage(format string, args ...interface{}) string {
	message := fmt.Sprintf(format, args...)
	if len(cl.fields) > 0 {
		var fieldStrs []string
		for k, v := range cl.fields {
			fieldStrs = append(fieldStrs, fmt.Sprintf("%s=%v", k, v))
		}
		message = fmt.Sprintf("%s | %s", message, strings.Join(fieldStrs, " "))
	}
	return message
}

// Debug logs a debug message with context fields
func (cl *ContextLogger) Debug(format string, args ...interface{}) {
	cl.logger.logMessage(DEBUG, cl.formatMessage(format, args...))
}

// Verbose logs a verbose message with context fields
func (cl *ContextLogger) Verbose(format string, args ...interface{}) {
	cl.logger.logMessage(VERBOSE, cl.formatMessage(format, args...))
}

// Info logs an info message with context fields
func (cl *ContextLogger) Info(format string, args ...interface{}) {
	cl.logger.logMessage(INFO, cl.formatMessage(format, args...))
}

// Warning logs a warning message with context fields
func (cl *ContextLogger) Warning(format string, args ...interface{}) {
	cl.logger.logMessage(WARNING, cl.formatMessage(format, args...))
}

// Error logs an error message with context fields
func (cl *ContextLogger) Error(format string, args ...interface{}) {
	cl.logger.logMessage(ERROR, cl.formatMessage(format, args...))
}

// WithField adds a field to the context and returns a new ContextLogger
func (cl *ContextLogger) WithField(key string, value interface{}) *ContextLogger {
	newFields := make(map[string]interface{})
	for k, v := range cl.fields {
		newFields[k] = v
	}
	newFields[key] = value
	return &ContextLogger{
		logger: cl.logger,
		fields: newFields,
	}
}
