// Package database owns PostgreSQL pool construction and lifecycle defaults.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxConnections    = 4
	pingTimeout       = 5 * time.Second
	maxConnectionAge  = 30 * time.Minute
	maxIdleConnection = 5 * time.Minute
)

// Open validates and pings a bounded PostgreSQL pool before returning it.
// Callers own the returned pool and must close it after work drains.
func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database connection: %w", err)
	}

	poolConfig.MaxConns = maxConnections
	poolConfig.MinConns = 0
	poolConfig.MaxConnLifetime = maxConnectionAge
	poolConfig.MaxConnIdleTime = maxIdleConnection
	poolConfig.PingTimeout = pingTimeout

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("open database pool: %w", err)
	}

	pingContext, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingContext); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}
