// Command db owns explicit local/test migration and seed operations. The API
// process opens a database only when its development identity mode is enabled.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/config"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/database"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/databaseconfig"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/migrations"
)

const commandTimeout = 30 * time.Second

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "database command error: %s\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("database command is required (migrate up|down|status or seed)")
	}

	configuration, err := databaseconfig.Load()
	if err != nil {
		return err
	}

	switch args[0] {
	case "migrate":
		if len(args) != 2 {
			return errors.New("migrate requires exactly one operation: up, down, or status")
		}
		if args[1] == "down" && configuration.Environment != config.Test {
			return errors.New("migrate down is allowed only for the isolated test database")
		}
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		return migrations.Run(ctx, configuration.URL, args[1])

	case "seed":
		if len(args) != 1 {
			return errors.New("seed does not accept additional arguments")
		}
		if configuration.Environment != config.Development && configuration.Environment != config.Test {
			return errors.New("seed is allowed only for development or isolated test databases")
		}
		return seedControlledOrganization(configuration)

	default:
		return fmt.Errorf("unsupported database command %q (want migrate or seed)", args[0])
	}
}

func seedControlledOrganization(configuration databaseconfig.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	pool, err := database.Open(ctx, configuration.URL)
	if err != nil {
		return err
	}
	defer pool.Close()

	repository, err := registry.NewRepository(pool)
	if err != nil {
		return err
	}
	organizationID, err := repository.EnsureOrganization(ctx, "pulsegrid-dev", "Pulsegrid Development")
	if err != nil {
		return fmt.Errorf("ensure development organization: %w", err)
	}
	fmt.Printf("development organization ready: %s\n", organizationID)
	return nil
}
