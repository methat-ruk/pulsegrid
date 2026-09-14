// Package migrations embeds the SQL schema owned by the persistence foundation.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// FS contains immutable SQL migration files shipped with the API module.
//
//go:embed *.sql
var FS embed.FS

// Run applies one explicit migration command to the selected local/test
// database. The advisory session lock prevents concurrent migration runners.
func Run(ctx context.Context, databaseURL string, operation string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("create migration lock: %w", err)
	}
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		FS,
		goose.WithSessionLocker(locker),
	)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	defer provider.Close()

	switch strings.ToLower(strings.TrimSpace(operation)) {
	case "up":
		_, err = provider.Up(ctx)
	case "down":
		_, err = provider.Down(ctx)
	case "status":
		statuses, statusErr := provider.Status(ctx)
		if statusErr != nil {
			return statusErr
		}
		for _, status := range statuses {
			fmt.Printf("%d %s %s\n", status.Source.Version, status.State, status.Source.Path)
		}
		return nil
	default:
		return fmt.Errorf("unsupported migration operation %q (want up, down, or status)", operation)
	}
	if err != nil {
		return fmt.Errorf("migration %s: %w", operation, err)
	}
	return nil
}
