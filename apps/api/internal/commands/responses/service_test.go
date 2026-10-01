package responses

import (
	"context"
	"errors"
	"testing"

	"github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/commands"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/commands/mqttcontract"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/commands/mqtttransport"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
)

type fakeResolver struct {
	organizationID uuid.UUID
	err            error
	tenant         string
	deviceID       uuid.UUID
}

func (r *fakeResolver) ResolveDevice(_ context.Context, tenant string, deviceID uuid.UUID) (uuid.UUID, error) {
	r.tenant, r.deviceID = tenant, deviceID
	return r.organizationID, r.err
}

type fakeRepository struct {
	organizationID uuid.UUID
	deviceID       uuid.UUID
	commandID      uuid.UUID
	event          commands.Event
	failureCode    commands.FailureCode
	result         commands.Command
	err            error
	calls          int
}

func (r *fakeRepository) ApplyDeviceResponse(_ context.Context, organizationID, deviceID, commandID uuid.UUID, event commands.Event, failureCode commands.FailureCode) (commands.Command, error) {
	r.calls++
	r.organizationID, r.deviceID, r.commandID = organizationID, deviceID, commandID
	r.event, r.failureCode = event, failureCode
	return r.result, r.err
}

type unusedClient struct{}

func (unusedClient) Connect() mqtttransport.Token { return nil }
func (unusedClient) Subscribe(string, byte, func(mqtttransport.Message)) mqtttransport.SubscribeToken {
	return nil
}
func (unusedClient) Publish(string, byte, bool, any) mqtttransport.Token { return nil }
func (unusedClient) Disconnect(uint)                                     {}
func (unusedClient) IsConnectionOpen() bool                              { return false }

func newServiceForTest(t *testing.T, resolver DeviceResolver, repository Repository) *Service {
	t.Helper()
	transport, err := mqtttransport.New(mqtttransport.DefaultConfig("mqtt://127.0.0.1:11883"), func(*mqtt.ClientOptions) mqtttransport.Client { return unusedClient{} }, nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(resolver, repository, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestProcessBindsTopicTenantDeviceAndCommandBeforeStateTransition(t *testing.T) {
	organizationID := uuid.New()
	deviceID := uuid.New()
	commandID := uuid.New()
	resolver := &fakeResolver{organizationID: organizationID}
	repository := &fakeRepository{result: commands.Command{ID: commandID, Status: commands.StatusCompleted}}
	service := newServiceForTest(t, resolver, repository)
	payload, err := mqttcontract.EncodeResponse(commandID, mqttcontract.OutcomeCompleted, "")
	if err != nil {
		t.Fatal(err)
	}
	service.process(mqtttransport.Delivery{
		ID: uuid.New(), Topic: "pulsegrid/v1/tenants/pulsegrid-dev/devices/" + deviceID.String() + "/command-responses",
		Payload: payload, PayloadBytes: len(payload), QoS: 1,
	})
	if repository.calls != 1 || repository.organizationID != organizationID || repository.deviceID != deviceID || repository.commandID != commandID || repository.event != commands.EventComplete {
		t.Fatalf("applied response = %+v", repository)
	}
	if resolver.tenant != "pulsegrid-dev" || resolver.deviceID != deviceID {
		t.Fatalf("resolved identity = (%q, %s)", resolver.tenant, resolver.deviceID)
	}
}

func TestProcessMapsAllDeviceOutcomesToDomainEvents(t *testing.T) {
	for _, test := range []struct {
		outcome     mqttcontract.Outcome
		failureCode string
		event       commands.Event
		failure     commands.FailureCode
	}{
		{outcome: mqttcontract.OutcomeAck, event: commands.EventAck},
		{outcome: mqttcontract.OutcomeCompleted, event: commands.EventComplete},
		{outcome: mqttcontract.OutcomeFailed, failureCode: mqttcontract.DeviceFailureCode, event: commands.EventFail, failure: commands.FailureDeviceReported},
	} {
		t.Run(string(test.outcome), func(t *testing.T) {
			commandID, deviceID := uuid.New(), uuid.New()
			resolver := &fakeResolver{organizationID: uuid.New()}
			repository := &fakeRepository{}
			service := newServiceForTest(t, resolver, repository)
			payload, err := mqttcontract.EncodeResponse(commandID, test.outcome, test.failureCode)
			if err != nil {
				t.Fatalf("encode response: %v", err)
			}
			service.process(mqtttransport.Delivery{
				ID: uuid.New(), Topic: "pulsegrid/v1/tenants/pulsegrid-dev/devices/" + deviceID.String() + "/command-responses",
				Payload: payload, PayloadBytes: len(payload), QoS: 1,
			})
			if repository.calls != 1 || repository.event != test.event || repository.failureCode != test.failure {
				t.Fatalf("mapped response = event %s failure %s calls %d", repository.event, repository.failureCode, repository.calls)
			}
		})
	}
}

func TestProcessRejectsInvalidTransportTopicPayloadAndUnregisteredDevice(t *testing.T) {
	commandID, deviceID := uuid.New(), uuid.New()
	tests := []struct {
		name     string
		mutate   func(*mqtttransport.Delivery)
		resolver *fakeResolver
	}{
		{name: "wrong qos", mutate: func(d *mqtttransport.Delivery) { d.QoS = 0 }},
		{name: "retained", mutate: func(d *mqtttransport.Delivery) { d.Retained = true }},
		{name: "oversized", mutate: func(d *mqtttransport.Delivery) { d.Oversized = true }},
		{name: "invalid topic", mutate: func(d *mqtttransport.Delivery) { d.Topic = "not/a/response" }},
		{name: "invalid payload", mutate: func(d *mqtttransport.Delivery) { d.Payload = []byte("{}") }},
		{name: "unregistered device", resolver: &fakeResolver{err: registry.ErrNotFound}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := test.resolver
			if resolver == nil {
				resolver = &fakeResolver{organizationID: uuid.New()}
			}
			repository := &fakeRepository{}
			service := newServiceForTest(t, resolver, repository)
			payload, err := mqttcontract.EncodeResponse(commandID, mqttcontract.OutcomeAck, "")
			if err != nil {
				t.Fatal(err)
			}
			delivery := mqtttransport.Delivery{
				ID: uuid.New(), Topic: "pulsegrid/v1/tenants/pulsegrid-dev/devices/" + deviceID.String() + "/command-responses",
				Payload: payload, PayloadBytes: len(payload), QoS: 1,
			}
			if test.mutate != nil {
				test.mutate(&delivery)
			}
			service.process(delivery)
			if repository.calls != 0 {
				t.Fatalf("rejected delivery changed command: %+v", repository)
			}
		})
	}
}

func TestProcessKeepsTimedOutCommandTerminal(t *testing.T) {
	commandID, deviceID := uuid.New(), uuid.New()
	resolver := &fakeResolver{organizationID: uuid.New()}
	repository := &fakeRepository{err: commands.ErrTimedOut}
	service := newServiceForTest(t, resolver, repository)
	payload, err := mqttcontract.EncodeResponse(commandID, mqttcontract.OutcomeCompleted, "")
	if err != nil {
		t.Fatal(err)
	}
	service.process(mqtttransport.Delivery{
		ID: uuid.New(), Topic: "pulsegrid/v1/tenants/pulsegrid-dev/devices/" + deviceID.String() + "/command-responses",
		Payload: payload, PayloadBytes: len(payload), QoS: 1,
	})
	if repository.calls != 1 || repository.event != commands.EventComplete || !errors.Is(repository.err, commands.ErrTimedOut) {
		t.Fatalf("timeout response call = %+v", repository)
	}
}
