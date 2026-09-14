package database

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOpenRejectsInvalidConnectionURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := Open(ctx, "postgres://%zz")
	if err == nil || !strings.Contains(err.Error(), "parse database connection") {
		t.Fatalf("Open error = %v, want a connection URL parse error", err)
	}
}
