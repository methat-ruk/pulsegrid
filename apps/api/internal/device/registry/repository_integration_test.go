//go:build integration

// These tests require the disposable PostgreSQL service owned by
// `api:test:integration`; keep them out of the default no-database test suite.

package registry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/config"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/database"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/databaseconfig"
)

func TestRepositoryTenantIsolationAndKeysetPagination(t *testing.T) {
	repository, cleanup := integrationRepository(t)
	defer cleanup()
	ctx := context.Background()

	orgA := createIntegrationOrganization(t, repository, "A")
	orgB := createIntegrationOrganization(t, repository, "B")
	defer cleanupOrganizations(t, repository, orgA, orgB)

	if _, err := repository.CreateDevice(ctx, orgA, CreateDeviceInput{DeviceKey: "shared-key", DisplayName: "A shared"}); err != nil {
		t.Fatalf("create device in organization A: %v", err)
	}
	if _, err := repository.CreateDevice(ctx, orgB, CreateDeviceInput{DeviceKey: "shared-key", DisplayName: "B shared"}); err != nil {
		t.Fatalf("same device key in organization B: %v", err)
	}
	for index := 0; index < 3; index++ {
		if _, err := repository.CreateDevice(ctx, orgA, CreateDeviceInput{
			DeviceKey:   fmt.Sprintf("device-%d", index),
			DisplayName: fmt.Sprintf("A device %d", index),
		}); err != nil {
			t.Fatalf("create paged device %d: %v", index, err)
		}
	}

	devices, err := repository.ListDevices(ctx, orgA, 2, nil)
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if len(devices.Devices) != 2 || devices.NextCursor == nil {
		t.Fatalf("first page = %+v, want two devices and a cursor", devices)
	}
	for _, device := range devices.Devices {
		if device.OrganizationID != orgA {
			t.Fatalf("first page leaked organization %s device", device.OrganizationID)
		}
	}

	nextPage, err := repository.ListDevices(ctx, orgA, 2, devices.NextCursor)
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	if len(nextPage.Devices) != 2 || nextPage.NextCursor != nil {
		t.Fatalf("second page = %+v, want final two devices", nextPage)
	}
	seenKeys := make(map[string]struct{}, 4)
	allDevices := append(append([]Device{}, devices.Devices...), nextPage.Devices...)
	for index, device := range allDevices {
		seenKeys[device.DeviceKey] = struct{}{}
		if device.OrganizationID != orgA {
			t.Fatalf("page leaked organization %s device", device.OrganizationID)
		}
		if index > 0 {
			previous := allDevices[index-1]
			if previous.CreatedAt.Before(device.CreatedAt) || (previous.CreatedAt.Equal(device.CreatedAt) && bytes.Compare(previous.ID[:], device.ID[:]) < 0) {
				t.Fatalf("page order is not created_at DESC, id DESC: previous=%+v current=%+v", previous, device)
			}
		}
	}
	if len(seenKeys) != 4 {
		t.Fatalf("paged device keys = %v, want four unique keys", seenKeys)
	}

	var organizationBDevice Device
	organizationBPage, err := repository.ListDevices(ctx, orgB, 100, nil)
	if err != nil {
		t.Fatalf("list organization B: %v", err)
	}
	if len(organizationBPage.Devices) != 1 {
		t.Fatalf("organization B page = %+v, want one device", organizationBPage)
	}
	organizationBDevice = organizationBPage.Devices[0]
	if _, err := repository.GetDevice(ctx, orgA, organizationBDevice.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-organization get error = %v, want ErrNotFound", err)
	}
	if _, err := repository.CreateDevice(ctx, orgA, CreateDeviceInput{DeviceKey: "shared-key", DisplayName: "duplicate"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate key error = %v, want ErrConflict", err)
	}
	if _, err := repository.CreateDevice(ctx, uuid.New(), CreateDeviceInput{DeviceKey: "unknown-org", DisplayName: "invalid"}); !errors.Is(err, ErrInvalidOrganization) {
		t.Fatalf("unknown organization error = %v, want ErrInvalidOrganization", err)
	}
}

func TestRepositoryConcurrentDuplicateDeviceKey(t *testing.T) {
	repository, cleanup := integrationRepository(t)
	defer cleanup()
	orgID := createIntegrationOrganization(t, repository, "concurrent")
	defer cleanupOrganizations(t, repository, orgID)

	const workers = 8
	errorsCh := make(chan error, workers)
	var waitGroup sync.WaitGroup
	for index := 0; index < workers; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_, err := repository.CreateDevice(context.Background(), orgID, CreateDeviceInput{
				DeviceKey:   "concurrent-key",
				DisplayName: "Concurrent device",
			})
			errorsCh <- err
		}()
	}
	waitGroup.Wait()
	close(errorsCh)

	successes := 0
	conflicts := 0
	for err := range errorsCh {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent create error = %v, want conflict", err)
		}
	}
	if successes != 1 || conflicts != workers-1 {
		t.Fatalf("concurrent create results = %d successes, %d conflicts", successes, conflicts)
	}
}

func TestDatabaseConstraintsRejectInvalidRows(t *testing.T) {
	repository, cleanup := integrationRepository(t)
	defer cleanup()
	orgID := createIntegrationOrganization(t, repository, "constraints")
	defer cleanupOrganizations(t, repository, orgID)
	if _, err := repository.CreateDevice(context.Background(), orgID, CreateDeviceInput{DeviceKey: "protected", DisplayName: "Protected"}); err != nil {
		t.Fatalf("create protected device: %v", err)
	}
	if _, err := repository.pool.Exec(context.Background(), "DELETE FROM organizations WHERE id = $1", orgID); err == nil || !strings.Contains(err.Error(), "devices_organization_id_fkey") {
		t.Fatalf("delete organization error = %v, want foreign-key restriction", err)
	}

	_, err := repository.pool.Exec(context.Background(), `
		INSERT INTO devices (organization_id, device_key, display_name)
		VALUES ($1, $2, $3)
	`, orgID, " invalid ", "Invalid direct row")
	if err == nil || !strings.Contains(err.Error(), "devices_key_format") {
		t.Fatalf("invalid device key error = %v, want devices_key_format", err)
	}

	_, err = repository.pool.Exec(context.Background(), `
		INSERT INTO devices (organization_id, device_key, display_name)
		VALUES ($1, $2, $3)
	`, uuid.New(), "valid-key", "Unknown organization")
	if err == nil || !strings.Contains(err.Error(), "devices_organization_id_fkey") {
		t.Fatalf("unknown organization error = %v, want foreign-key violation", err)
	}
}

func TestRepositoryHonorsCanceledContext(t *testing.T) {
	repository, cleanup := integrationRepository(t)
	defer cleanup()
	orgID := createIntegrationOrganization(t, repository, "context")
	defer cleanupOrganizations(t, repository, orgID)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := repository.CreateDevice(ctx, orgID, CreateDeviceInput{DeviceKey: "canceled", DisplayName: "Canceled"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled create error = %v, want context.Canceled", err)
	}
}

func integrationRepository(t *testing.T) (*Repository, func()) {
	t.Helper()
	configuration, err := databaseconfig.Load()
	if err != nil {
		t.Fatalf("load integration database configuration: %v", err)
	}
	if configuration.Environment != config.Test {
		t.Fatalf("integration environment = %q, want test", configuration.Environment)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, configuration.URL)
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	repository, err := NewRepository(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("create repository: %v", err)
	}
	return repository, pool.Close
}

func createIntegrationOrganization(t *testing.T, repository *Repository, label string) uuid.UUID {
	t.Helper()
	slug := "integration-" + strings.ToLower(label) + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	organizationID, err := repository.CreateOrganization(context.Background(), slug, "Integration "+label)
	if err != nil {
		t.Fatalf("create integration organization: %v", err)
	}
	return organizationID
}

func cleanupOrganizations(t *testing.T, repository *Repository, organizationIDs ...uuid.UUID) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, organizationID := range organizationIDs {
		if _, err := repository.pool.Exec(ctx, "DELETE FROM devices WHERE organization_id = $1", organizationID); err != nil {
			t.Errorf("cleanup devices for %s: %v", organizationID, err)
		}
		if _, err := repository.pool.Exec(ctx, "DELETE FROM organizations WHERE id = $1", organizationID); err != nil {
			t.Errorf("cleanup organization %s: %v", organizationID, err)
		}
	}
}
