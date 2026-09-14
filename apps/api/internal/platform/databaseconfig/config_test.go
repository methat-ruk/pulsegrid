package databaseconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/config"
)

func TestParseAcceptsIsolatedDevelopmentTarget(t *testing.T) {
	got, err := Parse(map[string]string{
		"PULSEGRID_DATABASE_URL": "postgres://pulsegrid:local-secret@127.0.0.1:5432/pulsegrid_dev?sslmode=disable",
	}, config.Development)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got.Database != "pulsegrid_dev" || got.Port != 5432 || got.Username != "pulsegrid" {
		t.Fatalf("unexpected database config: %+v", got)
	}
}

func TestParseAcceptsIsolatedTestTarget(t *testing.T) {
	got, err := Parse(map[string]string{
		"PULSEGRID_DATABASE_URL": "postgresql://pulsegrid:test-secret@127.0.0.1:15432/pulsegrid_test?sslmode=disable",
	}, config.Test)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got.Database != "pulsegrid_test" || got.Port != 15432 {
		t.Fatalf("unexpected test database config: %+v", got)
	}
}

func TestParseRejectsUnsafeTargets(t *testing.T) {
	tests := []struct {
		name string
		env  config.Environment
		url  string
		want string
	}{
		{
			name: "placeholder",
			env:  config.Development,
			url:  "postgres://pulsegrid:CHANGE_ME@127.0.0.1:5432/pulsegrid_dev?sslmode=disable",
			want: "CHANGE_ME",
		},
		{
			name: "development target in test",
			env:  config.Test,
			url:  "postgres://pulsegrid:test-secret@127.0.0.1:5432/pulsegrid_dev?sslmode=disable",
			want: "port 15432",
		},
		{
			name: "remote host",
			env:  config.Development,
			url:  "postgres://pulsegrid:local-secret@db.example.test:5432/pulsegrid_dev?sslmode=disable",
			want: "127.0.0.1",
		},
		{
			name: "production",
			env:  config.Production,
			url:  "postgres://pulsegrid:prod-secret@db.example.test:5432/pulsegrid_dev?sslmode=disable",
			want: "production",
		},
		{
			name: "extra query",
			env:  config.Development,
			url:  "postgres://pulsegrid:local-secret@127.0.0.1:5432/pulsegrid_dev?sslmode=disable&connect_timeout=2",
			want: "unsupported query",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse(map[string]string{"PULSEGRID_DATABASE_URL": test.url}, test.env)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Parse error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestLoadFromMergesSelectedDotenvWithoutOverridingProcess(t *testing.T) {
	process := map[string]string{
		"PULSEGRID_ENV": "test",
	}
	readDotenv := func(path string) (map[string]string, error) {
		if path != filepath.Join("/tmp/config", ".env.test") {
			t.Fatalf("dotenv path = %q", path)
		}
		return map[string]string{
			"PULSEGRID_DATABASE_URL": "postgres://pulsegrid:test-secret@127.0.0.1:15432/pulsegrid_test?sslmode=disable",
		}, nil
	}

	got, err := LoadFrom(process, "/tmp/config", readDotenv)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if got.Database != "pulsegrid_test" {
		t.Fatalf("database = %q, want pulsegrid_test", got.Database)
	}
}

func TestLoadFromReadsDatabaseURLFromIgnoredDotenv(t *testing.T) {
	workingDirectory := t.TempDir()
	dotenv := "PULSEGRID_DATABASE_URL=postgres://pulsegrid:local-secret@127.0.0.1:5432/pulsegrid_dev?sslmode=disable\n"
	if err := os.WriteFile(filepath.Join(workingDirectory, ".env.development"), []byte(dotenv), 0o600); err != nil {
		t.Fatalf("write dotenv fixture: %v", err)
	}

	got, err := LoadFrom(map[string]string{"PULSEGRID_ENV": "development"}, workingDirectory, nil)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if got.URL != strings.TrimSpace(dotenv[len("PULSEGRID_DATABASE_URL="):]) {
		t.Fatalf("unexpected URL: %q", got.URL)
	}
}
