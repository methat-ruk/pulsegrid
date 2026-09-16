// Package databaseconfig owns the explicit database-command configuration
// boundary. The health-only API does not load this package at startup.
package databaseconfig

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"

	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/config"
)

const (
	databaseURLKey  = "PULSEGRID_DATABASE_URL"
	localHost       = "127.0.0.1"
	developmentDB   = "pulsegrid_dev"
	testDB          = "pulsegrid_test"
	developmentPort = 5432
	testPort        = 15432
)

// Config is the validated connection target for database commands and tests.
// URL is retained only for the selected database client and is never logged.
type Config struct {
	Environment config.Environment
	URL         string
	Host        string
	Port        int
	Database    string
	Username    string
}

// DotenvReader is injectable for deterministic configuration tests.
type DotenvReader func(path string) (map[string]string, error)

// Load reads process configuration and merges the selected local dotenv file
// for development or test. Process values always win.
func Load() (Config, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return Config{}, fmt.Errorf("get database configuration directory: %w", err)
	}

	return LoadFrom(environmentFromOS(), workingDirectory, readLocalDotenv)
}

// LoadFrom parses a deterministic environment map and selected dotenv source.
func LoadFrom(processEnvironment map[string]string, workingDirectory string, readDotenv DotenvReader) (Config, error) {
	rawEnvironment, ok := processEnvironment["PULSEGRID_ENV"]
	if !ok || strings.TrimSpace(rawEnvironment) == "" {
		return Config{}, errors.New("database configuration PULSEGRID_ENV is required")
	}

	environment, err := config.ParseEnvironment(rawEnvironment)
	if err != nil {
		return Config{}, err
	}

	values := cloneValues(processEnvironment)
	if environment != config.Production {
		if readDotenv == nil {
			readDotenv = readLocalDotenv
		}
		if workingDirectory == "" {
			workingDirectory = "."
		}

		dotenvPath := filepath.Join(workingDirectory, ".env."+string(environment))
		dotenvValues, readErr := readDotenv(dotenvPath)
		if readErr != nil && !os.IsNotExist(readErr) {
			return Config{}, fmt.Errorf("load %s database configuration: %w", string(environment), readErr)
		}
		for key, value := range dotenvValues {
			if _, exists := processEnvironment[key]; !exists {
				values[key] = value
			}
		}
	}

	return Parse(values, environment)
}

// Parse validates an explicit local/test database target. Production database
// operations are intentionally outside this MVP slice.
func Parse(values map[string]string, environment config.Environment) (Config, error) {
	if environment == config.Production {
		return Config{}, errors.New("database commands do not support production targets")
	}

	rawURL := strings.TrimSpace(values[databaseURLKey])
	if rawURL == "" {
		return Config{}, errors.New("database configuration PULSEGRID_DATABASE_URL is required")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return Config{}, errors.New("database configuration PULSEGRID_DATABASE_URL is invalid")
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return Config{}, errors.New("database configuration PULSEGRID_DATABASE_URL must use postgres or postgresql")
	}
	if parsed.Hostname() != localHost || parsed.Port() == "" {
		return Config{}, fmt.Errorf("database configuration PULSEGRID_DATABASE_URL must target %s", localHost)
	}
	if parsed.Fragment != "" || parsed.RawQuery == "" || parsed.Query().Get("sslmode") != "disable" {
		return Config{}, errors.New("database configuration PULSEGRID_DATABASE_URL must set sslmode=disable without a fragment")
	}
	if parsed.User == nil {
		return Config{}, errors.New("database configuration PULSEGRID_DATABASE_URL must include a database user")
	}
	username := strings.TrimSpace(parsed.User.Username())
	password, hasPassword := parsed.User.Password()
	if username == "" || !hasPassword || strings.TrimSpace(password) == "" {
		return Config{}, errors.New("database configuration PULSEGRID_DATABASE_URL must include a database password")
	}
	if password == "CHANGE_ME" {
		return Config{}, errors.New("database configuration PULSEGRID_DATABASE_URL still contains the CHANGE_ME placeholder")
	}

	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		return Config{}, errors.New("database configuration PULSEGRID_DATABASE_URL has an invalid port")
	}
	expectedPort, expectedDatabase := targetFor(environment)
	if port != expectedPort {
		return Config{}, fmt.Errorf("database configuration PULSEGRID_DATABASE_URL must use port %d for %s", expectedPort, environment)
	}

	databaseName := strings.TrimPrefix(parsed.Path, "/")
	if databaseName != expectedDatabase || databaseName == "" || strings.Contains(databaseName, "/") {
		return Config{}, fmt.Errorf("database configuration PULSEGRID_DATABASE_URL must use database %s for %s", expectedDatabase, environment)
	}
	if queryKeys := parsed.Query(); len(queryKeys) != 1 || len(queryKeys["sslmode"]) != 1 {
		return Config{}, errors.New("database configuration PULSEGRID_DATABASE_URL has unsupported query parameters")
	}
	if parsed.User.Username() != "pulsegrid" {
		return Config{}, errors.New("database configuration PULSEGRID_DATABASE_URL must use the pulsegrid local user")
	}

	return Config{
		Environment: environment,
		URL:         rawURL,
		Host:        localHost,
		Port:        port,
		Database:    databaseName,
		Username:    username,
	}, nil
}

func targetFor(environment config.Environment) (int, string) {
	if environment == config.Test {
		return testPort, testDB
	}
	return developmentPort, developmentDB
}

func readLocalDotenv(path string) (map[string]string, error) {
	return godotenv.Read(path)
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
	maps.Copy(clone, values)
	return clone
}
