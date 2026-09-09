// Package httpserver owns the API process HTTP transport and lifecycle.
package httpserver

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/config"
)

const (
	LivePath  = "/health/live"
	ReadyPath = "/health/ready"

	requestIDHeader = "X-Request-ID"
	maxBodySize     = 64 * 1024
	readTimeout     = 5 * time.Second
	writeTimeout    = 10 * time.Second
	idleTimeout     = 60 * time.Second

	lifecycleStarting uint32 = iota
	lifecycleReady
	lifecycleDraining
	lifecycleStopped
)

type statusResponse struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type errorResponse struct {
	Error publicError `json:"error"`
}

type publicError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Server is the lifecycle owner for the API HTTP process.
type Server struct {
	app               *fiber.App
	address           string
	logger            *slog.Logger
	state             atomic.Uint32
	readySignal       chan struct{}
	readySignalOnce   sync.Once
	shutdownLogOnce   sync.Once
	stoppedSignal     chan struct{}
	stoppedSignalOnce sync.Once
}

// New creates an HTTP server without starting a listener.
func New(cfg config.Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	server := &Server{
		address:       cfg.Address(),
		logger:        logger,
		readySignal:   make(chan struct{}),
		stoppedSignal: make(chan struct{}),
	}
	server.app = fiber.New(fiber.Config{
		BodyLimit:    maxBodySize,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
		ErrorHandler: server.errorHandler,
	})
	server.app.Use(server.requestIDMiddleware)
	server.app.Get(LivePath, server.livenessHandler)
	server.app.Get(ReadyPath, server.readinessHandler)

	server.app.Hooks().OnListen(func(data fiber.ListenData) error {
		server.state.Store(lifecycleReady)
		server.readySignalOnce.Do(func() { close(server.readySignal) })
		server.logger.Info("http server ready", "host", data.Host, "port", data.Port)
		return nil
	})
	server.app.Hooks().OnPreShutdown(func() error {
		server.state.Store(lifecycleDraining)
		server.logger.Info("http server draining", "reason_code", "shutdown_requested")
		return nil
	})
	server.app.Hooks().OnPostShutdown(func(err error) error {
		server.state.Store(lifecycleStopped)
		server.shutdownLogOnce.Do(func() {
			if err != nil {
				server.logger.Error("http server shutdown failed", "reason_code", "shutdown_failed")
			} else {
				server.logger.Info("http server stopped", "reason_code", "shutdown_complete")
			}
		})
		server.stoppedSignalOnce.Do(func() { close(server.stoppedSignal) })
		return nil
	})

	return server
}

// Listen starts serving until the context is cancelled or the listener fails.
// Fiber owns the stop-admission and bounded-drain sequence through the supplied
// graceful context and shutdown timeout.
func (s *Server) Listen(ctx context.Context, shutdownTimeout time.Duration) error {
	return s.app.Listen(s.address, fiber.ListenConfig{
		DisableStartupMessage: true,
		GracefulContext:       ctx,
		ShutdownTimeout:       shutdownTimeout,
	})
}

// Ready reports whether the process is accepting normal traffic.
func (s *Server) Ready() bool {
	return s.state.Load() == lifecycleReady
}

// ReadySignal closes once the listener has started. It is intended for tests
// and process composition that must distinguish startup from configuration.
func (s *Server) ReadySignal() <-chan struct{} {
	return s.readySignal
}

// StoppedSignal closes after Fiber has completed its post-shutdown hooks.
// It is intended for lifecycle tests and process composition.
func (s *Server) StoppedSignal() <-chan struct{} {
	return s.stoppedSignal
}

func (s *Server) livenessHandler(c fiber.Ctx) error {
	return c.Status(http.StatusOK).JSON(statusResponse{Status: "ok"})
}

func (s *Server) readinessHandler(c fiber.Ctx) error {
	switch s.state.Load() {
	case lifecycleReady:
		return c.Status(http.StatusOK).JSON(statusResponse{Status: "ready"})
	case lifecycleDraining:
		return c.Status(http.StatusServiceUnavailable).JSON(statusResponse{
			Status: "not_ready",
			Reason: "draining",
		})
	default:
		return c.Status(http.StatusServiceUnavailable).JSON(statusResponse{
			Status: "not_ready",
			Reason: "starting",
		})
	}
}

func (s *Server) requestIDMiddleware(c fiber.Ctx) error {
	requestID := c.Get(requestIDHeader)
	if !isSafeRequestID(requestID) {
		requestID = newRequestID()
	}
	if requestID != "" {
		c.Set(requestIDHeader, requestID)
	}
	return c.Next()
}

func (s *Server) errorHandler(c fiber.Ctx, err error) error {
	statusCode := http.StatusInternalServerError
	publicCode := "internal_error"
	publicMessage := "internal server error"

	var fiberError *fiber.Error
	if errors.As(err, &fiberError) {
		switch fiberError.Code {
		case http.StatusBadRequest:
			statusCode = http.StatusBadRequest
			publicCode = "bad_request"
			publicMessage = "bad request"
		case http.StatusNotFound:
			statusCode = http.StatusNotFound
			publicCode = "not_found"
			publicMessage = "route not found"
		case http.StatusMethodNotAllowed:
			statusCode = http.StatusMethodNotAllowed
			publicCode = "method_not_allowed"
			publicMessage = "method not allowed"
		default:
			if fiberError.Code >= 400 && fiberError.Code < 500 {
				statusCode = fiberError.Code
				publicCode = "request_rejected"
				publicMessage = "request rejected"
			}
		}
	}

	logAttributes := []any{
		"status", statusCode,
		"reason_code", publicCode,
	}
	if requestID := c.Get(requestIDHeader); isSafeRequestID(requestID) {
		logAttributes = append(logAttributes, "request_id", requestID)
	}
	s.logger.Error("http request failed", logAttributes...)

	return c.Status(statusCode).JSON(errorResponse{
		Error: publicError{Code: publicCode, Message: publicMessage},
	})
}

func isSafeRequestID(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			!strings.ContainsRune("._-", character) {
			return false
		}
	}
	return true
}

func newRequestID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return ""
	}
	const hex = "0123456789abcdef"
	result := make([]byte, len(bytes)*2)
	for index, value := range bytes {
		result[index*2] = hex[value>>4]
		result[index*2+1] = hex[value&0x0f]
	}
	return string(result)
}
