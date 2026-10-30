// Package logging builds the single *zap.SugaredLogger used across graph-go.
// A logger is constructed in main.go and threaded down via constructors.
package logging

import (
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/term"
)

// New builds a SugaredLogger for the given level and format.
// Valid levels: "debug", "info", "warn", "error". Valid formats: "console", "json".
func New(level, format string) (*zap.SugaredLogger, error) {
	lvl, err := parseLevel(level)
	if err != nil {
		return nil, err
	}

	var cfg zap.Config
	switch strings.ToLower(format) {
	case "", "console":
		cfg = zap.NewDevelopmentConfig()
		// Colour only for a person at a terminal. Piped stderr (docker logs,
		// MCP clients, CI) would show the escape codes as text.
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
		if term.IsTerminal(int(os.Stderr.Fd())) {
			cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		}
	case "json":
		cfg = zap.NewProductionConfig()
	default:
		return nil, fmt.Errorf("invalid log format %q (want console|json)", format)
	}
	cfg.Level = zap.NewAtomicLevelAt(lvl)
	cfg.DisableStacktrace = lvl > zapcore.ErrorLevel

	// The development config attaches a stack trace to every warning, which
	// buries routine ones like "adapter register failed". Keep them for errors.
	l, err := cfg.Build(zap.AddStacktrace(zapcore.ErrorLevel))
	if err != nil {
		return nil, fmt.Errorf("build logger: %w", err)
	}
	return l.Sugar(), nil
}

// Nop returns a no-op logger suitable for tests.
func Nop() *zap.SugaredLogger { return zap.NewNop().Sugar() }

func parseLevel(s string) (zapcore.Level, error) {
	switch strings.ToLower(s) {
	case "", "info":
		return zapcore.InfoLevel, nil
	case "debug":
		return zapcore.DebugLevel, nil
	case "warn", "warning":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	default:
		return 0, fmt.Errorf("invalid log level %q (want debug|info|warn|error)", s)
	}
}
