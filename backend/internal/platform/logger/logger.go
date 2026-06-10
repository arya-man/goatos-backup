// Package logger is a thin shim kept for backwards compatibility.
// New code must use platform/observability.New directly.
package logger

import (
	"log/slog"

	"github.com/vgoats/goatos/backend/internal/platform/observability"
)

// New returns the process logger used by API commands and middleware.
// Deprecated: call observability.New instead for new entrypoints.
func New(level string) *slog.Logger {
	return observability.New(observability.Config{Level: level})
}
