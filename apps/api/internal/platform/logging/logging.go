// Package logging creates the process logger for each runtime environment.
package logging

import (
	"io"
	"log/slog"
	"os"

	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/config"
)

// New returns a structured logger. Development and test use readable text
// records; production uses JSON records for ingestion by a log platform.
func New(environment config.Environment, level slog.Level, writer io.Writer) *slog.Logger {
	if writer == nil {
		writer = os.Stdout
	}

	options := &slog.HandlerOptions{Level: level}
	if environment == config.Production {
		return slog.New(slog.NewJSONHandler(writer, options))
	}

	return slog.New(slog.NewTextHandler(writer, options))
}
