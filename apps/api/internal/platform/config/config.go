// Package config owns the API process configuration contract.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	defaultDevelopmentHost = "127.0.0.1"
	defaultDevelopmentPort = 8080
	defaultTestPort        = 18080
	defaultLogLevel        = "info"
	defaultShutdownTimeout = 10 * time.Second
	minimumShutdownTimeout = 1 * time.Second
	maximumShutdownTimeout = 2 * time.Minute
)

// Environment identifies the runtime mode selected by the operator or runner.
type Environment string

const (
	// Development is the local interactive runtime.
	Development Environment = "development"
	// Test is the isolated automated-test runtime.
	Test Environment = "test"
	// Production is the deployed runtime and must be explicitly configured.
	Production Environment = "production"
)

// Config contains only the settings consumed by the API foundation.
type Config struct {
	Environment     Environment
	HTTPHost        string
	HTTPPort        int
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
}

// Address returns the listener address for the configured host and port.
func (c Config) Address() string {
	return net.JoinHostPort(c.HTTPHost, strconv.Itoa(c.HTTPPort))
}

// DotenvReader is the injectable boundary used to read local environment
// files. Production never calls this boundary.
type DotenvReader func(path string) (map[string]string, error)

// Load reads process configuration and, for development or test only, fills
// missing values from the selected local dotenv file.
func Load() (Config, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return Config{}, fmt.Errorf("get configuration directory: %w", err)
	}

	return LoadFrom(environmentFromOS(), workingDirectory, readLocalDotenv)
}

// LoadFrom parses a deterministic environment map. Process values must be
// supplied separately from dotenv values so process configuration always wins.
func LoadFrom(processEnvironment map[string]string, workingDirectory string, readDotenv DotenvReader) (Config, error) {
	selectedEnvironment, ok := processEnvironment["PULSEGRID_ENV"]
	if !ok || strings.TrimSpace(selectedEnvironment) == "" {
		return Config{}, errors.New("configuration PULSEGRID_ENV is required")
	}

	environment, err := parseEnvironment(selectedEnvironment)
	if err != nil {
		return Config{}, err
	}

	values := cloneValues(processEnvironment)
	if environment != Production {
		if readDotenv == nil {
			readDotenv = readLocalDotenv
		}

		if workingDirectory == "" {
			workingDirectory = "."
		}

		dotenvPath := filepath.Join(workingDirectory, ".env."+string(environment))
		dotenvValues, readErr := readDotenv(dotenvPath)
		if readErr != nil && !os.IsNotExist(readErr) {
			return Config{}, fmt.Errorf("load %s configuration: %w", string(environment), readErr)
		}

		for key, value := range dotenvValues {
			if _, exists := processEnvironment[key]; !exists {
				values[key] = value
			}
		}
	}

	return parse(values, environment)
}

func readLocalDotenv(path string) (map[string]string, error) {
	return godotenv.Read(path)
}

func parse(values map[string]string, environment Environment) (Config, error) {
	host, err := parseHost(values, environment)
	if err != nil {
		return Config{}, err
	}

	port, err := parsePort(values, environment)
	if err != nil {
		return Config{}, err
	}

	logLevel, err := parseLogLevel(values, environment)
	if err != nil {
		return Config{}, err
	}

	shutdownTimeout, err := parseShutdownTimeout(values, environment)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Environment:     environment,
		HTTPHost:        host,
		HTTPPort:        port,
		LogLevel:        logLevel,
		ShutdownTimeout: shutdownTimeout,
	}, nil
}

func parseEnvironment(raw string) (Environment, error) {
	switch Environment(strings.ToLower(strings.TrimSpace(raw))) {
	case Development:
		return Development, nil
	case Test:
		return Test, nil
	case Production:
		return Production, nil
	default:
		return "", errors.New("configuration PULSEGRID_ENV must be development, test, or production")
	}
}

func parseHost(values map[string]string, environment Environment) (string, error) {
	host := strings.TrimSpace(values["PULSEGRID_HTTP_HOST"])
	if host == "" {
		if environment == Production {
			return "", errors.New("configuration PULSEGRID_HTTP_HOST is required in production")
		}
		return defaultDevelopmentHost, nil
	}

	if strings.ContainsAny(host, "\r\n\t /") || strings.ContainsAny(host, "[]") {
		return "", errors.New("configuration PULSEGRID_HTTP_HOST contains unsupported characters")
	}

	return host, nil
}

func parsePort(values map[string]string, environment Environment) (int, error) {
	rawPort := strings.TrimSpace(values["PULSEGRID_HTTP_PORT"])
	if rawPort == "" {
		switch environment {
		case Development:
			return defaultDevelopmentPort, nil
		case Test:
			return defaultTestPort, nil
		default:
			return 0, errors.New("configuration PULSEGRID_HTTP_PORT is required in production")
		}
	}

	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 1 || port > 65535 {
		return 0, errors.New("configuration PULSEGRID_HTTP_PORT must be between 1 and 65535")
	}

	return port, nil
}

func parseLogLevel(values map[string]string, environment Environment) (slog.Level, error) {
	rawLevel := strings.ToLower(strings.TrimSpace(values["PULSEGRID_LOG_LEVEL"]))
	if rawLevel == "" {
		if environment == Production {
			return 0, errors.New("configuration PULSEGRID_LOG_LEVEL is required in production")
		}
		rawLevel = defaultLogLevel
	}

	switch rawLevel {
	case "debug":
		if environment == Production {
			return 0, errors.New("configuration PULSEGRID_LOG_LEVEL debug is not allowed in production")
		}
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, errors.New("configuration PULSEGRID_LOG_LEVEL must be debug, info, warn, or error")
	}
}

func parseShutdownTimeout(values map[string]string, environment Environment) (time.Duration, error) {
	rawTimeout := strings.TrimSpace(values["PULSEGRID_SHUTDOWN_TIMEOUT"])
	if rawTimeout == "" {
		if environment == Production {
			return 0, errors.New("configuration PULSEGRID_SHUTDOWN_TIMEOUT is required in production")
		}
		return defaultShutdownTimeout, nil
	}

	shutdownTimeout, err := time.ParseDuration(rawTimeout)
	if err != nil || shutdownTimeout < minimumShutdownTimeout || shutdownTimeout > maximumShutdownTimeout {
		return 0, errors.New("configuration PULSEGRID_SHUTDOWN_TIMEOUT must be between 1s and 2m")
	}

	return shutdownTimeout, nil
}

func environmentFromOS() map[string]string {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[key] = value
		}
	}
	return values
}

func cloneValues(values map[string]string) map[string]string {
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}
