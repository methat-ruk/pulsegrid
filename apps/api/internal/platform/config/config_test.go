package config

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadFromUsesSafeDevelopmentDefaults(t *testing.T) {
	got, err := LoadFrom(map[string]string{"PULSEGRID_ENV": "development"}, t.TempDir(), missingDotenv)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}

	if got.Environment != Development {
		t.Fatalf("environment = %q, want %q", got.Environment, Development)
	}
	if got.Address() != "127.0.0.1:8080" {
		t.Fatalf("address = %q, want 127.0.0.1:8080", got.Address())
	}
	if got.LogLevel != slog.LevelInfo {
		t.Fatalf("log level = %v, want %v", got.LogLevel, slog.LevelInfo)
	}
	if got.ShutdownTimeout != 10*time.Second {
		t.Fatalf("shutdown timeout = %v, want 10s", got.ShutdownTimeout)
	}
}

func TestLoadFromUsesTestDefaults(t *testing.T) {
	got, err := LoadFrom(map[string]string{"PULSEGRID_ENV": "test"}, t.TempDir(), missingDotenv)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}

	if got.HTTPPort != 18080 {
		t.Fatalf("test port = %d, want 18080", got.HTTPPort)
	}
}

func TestLoadFromMergesDotenvWithoutOverridingProcessValues(t *testing.T) {
	process := map[string]string{
		"PULSEGRID_ENV":       "development",
		"PULSEGRID_HTTP_PORT": "9090",
	}
	readDotenv := func(path string) (map[string]string, error) {
		if path != filepath.Join("/tmp/config", ".env.development") {
			t.Fatalf("dotenv path = %q", path)
		}
		return map[string]string{
			"PULSEGRID_HTTP_HOST": "192.0.2.10",
			"PULSEGRID_HTTP_PORT": "7070",
			"PULSEGRID_LOG_LEVEL": "warn",
		}, nil
	}

	got, err := LoadFrom(process, "/tmp/config", readDotenv)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}

	if got.HTTPHost != "192.0.2.10" {
		t.Fatalf("host = %q, want dotenv host", got.HTTPHost)
	}
	if got.HTTPPort != 9090 {
		t.Fatalf("port = %d, want process port 9090", got.HTTPPort)
	}
	if got.LogLevel != slog.LevelWarn {
		t.Fatalf("log level = %v, want warn", got.LogLevel)
	}
}

func TestLoadFromReadsSelectedDotenvFile(t *testing.T) {
	workingDirectory := t.TempDir()
	dotenv := "PULSEGRID_HTTP_HOST=192.0.2.20\nPULSEGRID_HTTP_PORT=9091\nPULSEGRID_LOG_LEVEL=warn\n"
	if err := os.WriteFile(filepath.Join(workingDirectory, ".env.development"), []byte(dotenv), 0o600); err != nil {
		t.Fatalf("write dotenv fixture: %v", err)
	}

	got, err := LoadFrom(map[string]string{"PULSEGRID_ENV": "development"}, workingDirectory, nil)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if got.HTTPHost != "192.0.2.20" || got.HTTPPort != 9091 || got.LogLevel != slog.LevelWarn {
		t.Fatalf("unexpected dotenv config: %+v", got)
	}
}

func TestLoadFromDoesNotReadDotenvInProduction(t *testing.T) {
	called := false
	readDotenv := func(string) (map[string]string, error) {
		called = true
		return nil, errors.New("dotenv must not be read")
	}

	got, err := LoadFrom(map[string]string{
		"PULSEGRID_ENV":              "production",
		"PULSEGRID_HTTP_HOST":        "api.example.test",
		"PULSEGRID_HTTP_PORT":        "443",
		"PULSEGRID_LOG_LEVEL":        "info",
		"PULSEGRID_SHUTDOWN_TIMEOUT": "15s",
	}, t.TempDir(), readDotenv)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if called {
		t.Fatal("production configuration read a dotenv file")
	}
	if got.Environment != Production || got.HTTPPort != 443 {
		t.Fatalf("unexpected production config: %+v", got)
	}
}

func TestLoadFromRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "missing environment",
			env:  map[string]string{},
		},
		{
			name: "invalid environment",
			env:  map[string]string{"PULSEGRID_ENV": "sandbox"},
		},
		{
			name: "invalid host",
			env: map[string]string{
				"PULSEGRID_ENV":       "development",
				"PULSEGRID_HTTP_HOST": "127.0.0.1/health",
			},
		},
		{
			name: "invalid port",
			env: map[string]string{
				"PULSEGRID_ENV":       "development",
				"PULSEGRID_HTTP_PORT": "65536",
			},
		},
		{
			name: "invalid log level",
			env: map[string]string{
				"PULSEGRID_ENV":       "development",
				"PULSEGRID_LOG_LEVEL": "trace",
			},
		},
		{
			name: "invalid shutdown timeout",
			env: map[string]string{
				"PULSEGRID_ENV":              "development",
				"PULSEGRID_SHUTDOWN_TIMEOUT": "250ms",
			},
		},
		{
			name: "incomplete production",
			env:  map[string]string{"PULSEGRID_ENV": "production"},
		},
		{
			name: "production debug logging",
			env: map[string]string{
				"PULSEGRID_ENV":              "production",
				"PULSEGRID_HTTP_HOST":        "api.example.test",
				"PULSEGRID_HTTP_PORT":        "443",
				"PULSEGRID_LOG_LEVEL":        "debug",
				"PULSEGRID_SHUTDOWN_TIMEOUT": "15s",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := LoadFrom(test.env, t.TempDir(), missingDotenv); err == nil {
				t.Fatal("LoadFrom returned nil error for invalid configuration")
			}
		})
	}
}

func missingDotenv(string) (map[string]string, error) {
	return nil, os.ErrNotExist
}
