package runtime

import (
	"log/slog"
	"os"
	"strings"
)

// SetupLogger configures the slog default logger with a JSON handler
// at the specified level. Supported levels: debug, info, warn, error.
// Defaults to info if the level string is unrecognized.
func SetupLogger(level string) {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: lvl,
	})
	slog.SetDefault(slog.New(handler))
}
