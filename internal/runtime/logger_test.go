package runtime

import (
	"context"
	"log/slog"
	"testing"
)

func TestSetupLogger_RecognizesLevels(t *testing.T) {
	SetupLogger(" debug ")
	if !slog.Default().Handler().Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("expected debug level to enable debug logs")
	}

	SetupLogger("warning")
	if slog.Default().Handler().Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("expected warning level to disable info logs")
	}
	if !slog.Default().Handler().Enabled(context.Background(), slog.LevelWarn) {
		t.Fatal("expected warning level to enable warn logs")
	}
}

func TestSetupLogger_DefaultsToInfoForUnknownLevel(t *testing.T) {
	SetupLogger("unknown")
	if !slog.Default().Handler().Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("expected unknown level to default to info")
	}
	if slog.Default().Handler().Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("expected info level to disable debug logs")
	}
}
