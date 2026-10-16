// Package logger provides a structured logging wrapper around zerolog and lumberjack for log rotation.
package logger

import (
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Options defines the configuration for the logger.
type Options struct {
	File       string // File is the path to the log file. Empty disables file logging.
	MaxSizeMB  int    // MaxSizeMB is the maximum size in megabytes before rotation.
	MaxBackups int    // MaxBackups is the maximum number of old log files to retain.
	MaxAgeDays int    // MaxAgeDays is the maximum number of days to retain old log files.
	Compress   bool   // Compress determines if rotated files should be compressed.
	Level      string // Level is the minimum logging level (debug, info, warn, error, fatal).
	Console    bool   // Console if true, logs to stderr (in addition to the file, if set).
}

var (
	log *zerolog.Logger
	mu  sync.RWMutex
)

// Init initializes the global logger with the provided options.
// It sets up log rotation using lumberjack and structured logging with zerolog.
//
// Example:
//
//	logger.Init(logger.Options{
//		File: "/var/log/app.log",
//		Level: "info",
//		Console: true,
//	})
func Init(opts Options) {
	writers := []io.Writer{}
	if opts.Console {
		writers = append(writers, os.Stderr)
	}
	if opts.File != "" {
		writers = append(writers, &lumberjack.Logger{
			Filename:   opts.File,
			MaxSize:    opts.MaxSizeMB,
			MaxBackups: opts.MaxBackups,
			MaxAge:     opts.MaxAgeDays,
			Compress:   opts.Compress,
		})
	}
	if len(writers) == 0 {
		// never drop logs silently: fall back to stderr
		writers = append(writers, os.Stderr)
	}
	// JSON to every configured output
	out := zerolog.MultiLevelWriter(writers...)

	level := zerolog.InfoLevel
	if l, err := zerolog.ParseLevel(strings.ToLower(opts.Level)); err == nil {
		level = l
	}

	zerolog.TimeFieldFormat = time.RFC3339Nano
	zerolog.TimestampFieldName = "time"

	newLogger := zerolog.New(out).Level(level).With().Timestamp().Logger()

	mu.Lock()
	log = &newLogger
	mu.Unlock()

	log.Info().Msg("Logger initialized")
}

// get returns a thread-safe pointer to the global logger.
// If the logger is not initialized, it returns a default logger that writes to Stdout.
func get() *zerolog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	if log == nil {
		tmp := zerolog.New(os.Stdout).With().Timestamp().Logger()
		log = &tmp
	}
	return log
}

// --- Global shorthand functions ---

// Trace returns a zerolog.Event for trace level logging.
func Trace() *zerolog.Event { return get().Trace() }

// Debug returns a zerolog.Event for debug level logging.
func Debug() *zerolog.Event { return get().Debug() }

// Info returns a zerolog.Event for info level logging.
func Info() *zerolog.Event { return get().Info() }

// Warn returns a zerolog.Event for warning level logging.
func Warn() *zerolog.Event { return get().Warn() }

// Error returns a zerolog.Event for error level logging.
func Error() *zerolog.Event { return get().Error() }

// Fatal returns a zerolog.Event for fatal level logging.
// Calling this will exit the application with status code 1.
func Fatal() *zerolog.Event { return get().Fatal() }

// Panic returns a zerolog.Event for panic level logging.
// Calling this will cause the application to panic after logging.
func Panic() *zerolog.Event { return get().Panic() }
