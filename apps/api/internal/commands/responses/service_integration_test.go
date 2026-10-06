//go:build integration

package responses

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/commands"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/commands/mqttcontract"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/commands/mqtttransport"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/config"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/database"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/databaseconfig"
)

func TestProcessBindsCommandResponseToRegisteredTenantAndDevice(t *testing.T) {
	databaseConfiguration, err := databaseconfig.Load()
	if err != nil {
		t.Fatalf("load integration database configuration: %v", err)
	}
	if databaseConfiguration.Environment != config.Test {
		t.Fatalf("integration environment = %q, want test", databaseConfiguration.Environment)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, databaseConfiguration.URL)
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	t.Cleanup(pool.Close)
	registryRepository, err := registry.NewRepository(pool)
	if err != nil {
		t.Fatalf("create registry repository: %v", err)
	}
	commandRepository, err := commands.NewRepository(pool)
	if err != nil {
		t.Fatalf("create command repository: %v", err)
	}

	organizationIDs := make([]uuid.UUID, 0, 2)
	t.Cleanup(func() {
		if len(organizationIDs) == 0 {
			return
		}
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		transaction, err := pool.Begin(cleanupContext)
		if err != nil {
			t.Errorf("begin response-service fixture cleanup: %v", err)
			return
		}
		defer transaction.Rollback(cleanupContext)
		for _, statement := range []string{
			`DELETE FROM commands WHERE organization_id = ANY($1)`,
			`DELETE FROM devices WHERE organization_id = ANY($1)`,
			`DELETE FROM organizations WHERE id = ANY($1)`,
		} {
			if _, err := transaction.Exec(cleanupContext, statement, organizationIDs); err != nil {
				t.Errorf("delete response-service fixtures: %v", err)
				return
			}
		}
		var remaining int64
		if err := transaction.QueryRow(cleanupContext, `
			SELECT
				(SELECT count(*) FROM commands WHERE organization_id = ANY($1)) +
				(SELECT count(*) FROM devices WHERE organization_id = ANY($1)) +
				(SELECT count(*) FROM organizations WHERE id = ANY($1))
		`, organizationIDs).Scan(&remaining); err != nil {
			t.Errorf("verify response-service fixture cleanup: %v", err)
			return
		}
		if remaining != 0 {
			t.Errorf("response-service fixture cleanup left %d rows", remaining)
			return
		}
		if err := transaction.Commit(cleanupContext); err != nil {
			t.Errorf("commit response-service fixture cleanup: %v", err)
		}
	})
	tenantA := "response-a-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	tenantB := "response-b-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	organizationA, err := registryRepository.CreateOrganization(ctx, tenantA, "Response tenant A")
	if err != nil {
		t.Fatalf("create organization A: %v", err)
	}
	organizationIDs = append(organizationIDs, organizationA)
	organizationB, err := registryRepository.CreateOrganization(ctx, tenantB, "Response tenant B")
	if err != nil {
		t.Fatalf("create organization B: %v", err)
	}
	organizationIDs = append(organizationIDs, organizationB)
	deviceA, err := registryRepository.CreateDevice(ctx, organizationA, registry.CreateDeviceInput{DeviceKey: "response-device-a", DisplayName: "Response device A"})
	if err != nil {
		t.Fatalf("create device A: %v", err)
	}
	deviceB, err := registryRepository.CreateDevice(ctx, organizationB, registry.CreateDeviceInput{DeviceKey: "response-device-b", DisplayName: "Response device B"})
	if err != nil {
		t.Fatalf("create device B: %v", err)
	}
	commandA, err := commandRepository.Create(ctx, organizationA, commands.CreateInput{
		DeviceID: deviceA.ID, Type: commands.TypePing, IdempotencyKey: uuid.New(),
	})
	if err != nil {
		t.Fatalf("create tenant A command: %v", err)
	}
	commandB, err := commandRepository.Create(ctx, organizationB, commands.CreateInput{
		DeviceID: deviceB.ID, Type: commands.TypePing, IdempotencyKey: uuid.New(),
	})
	if err != nil {
		t.Fatalf("create tenant B command: %v", err)
	}

	var logOutput bytes.Buffer
	service := &Service{
		resolver:   registeredDeviceResolver{repository: registryRepository},
		repository: commandRepository,
		logger:     slog.New(slog.NewTextHandler(&logOutput, nil)),
		context:    ctx,
	}
	response, err := mqttcontract.EncodeResponse(commandA.ID, mqttcontract.OutcomeCompleted, "")
	if err != nil {
		t.Fatalf("encode cross-tenant device response: %v", err)
	}
	service.process(mqtttransport.Delivery{
		ID: uuid.New(), Topic: "pulsegrid/v1/tenants/" + tenantB + "/devices/" + deviceB.ID.String() + "/command-responses",
		Payload: response, PayloadBytes: len(response), QoS: 1,
	})
	if !strings.Contains(logOutput.String(), "reason_code=command_response_command_unmatched") ||
		!strings.Contains(logOutput.String(), "tenant="+tenantB) ||
		!strings.Contains(logOutput.String(), "device_id="+deviceB.ID.String()) ||
		!strings.Contains(logOutput.String(), "command_id="+commandA.ID.String()) {
		t.Fatalf("registered tenant B response did not fail A command binding: %s", logOutput.String())
	}
	stillPending, err := commandRepository.Get(ctx, organizationA, commandA.ID)
	if err != nil || stillPending.Status != commands.StatusPending {
		t.Fatalf("tenant A command after tenant B response = (%+v, %v), want unchanged PENDING", stillPending, err)
	}

	logOutput.Reset()
	validResponse, err := mqttcontract.EncodeResponse(commandB.ID, mqttcontract.OutcomeCompleted, "")
	if err != nil {
		t.Fatalf("encode tenant B device response: %v", err)
	}
	service.process(mqtttransport.Delivery{
		ID: uuid.New(), Topic: "pulsegrid/v1/tenants/" + tenantB + "/devices/" + deviceB.ID.String() + "/command-responses",
		Payload: validResponse, PayloadBytes: len(validResponse), QoS: 1,
	})
	completed, err := commandRepository.Get(ctx, organizationB, commandB.ID)
	if err != nil || completed.Status != commands.StatusCompleted || completed.AcknowledgedAt == nil || completed.TerminalAt == nil {
		t.Fatalf("valid tenant B response = (%+v, %v)", completed, err)
	}
}

type registeredDeviceResolver struct {
	repository *registry.Repository
}

func (resolver registeredDeviceResolver) ResolveDevice(ctx context.Context, tenantSlug string, deviceID uuid.UUID) (uuid.UUID, error) {
	device, err := resolver.repository.ResolveDeviceByTenantSlug(ctx, tenantSlug, deviceID)
	if err != nil {
		return uuid.Nil, err
	}
	return device.OrganizationID, nil
}
