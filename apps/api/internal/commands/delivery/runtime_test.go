package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/commands"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/commands/mqttcontract"
)

type fakeRepository struct {
	command     *commands.Command
	claimedOrg  uuid.UUID
	marked      int
	failed      int
	expiredOrg  uuid.UUID
	expiryBatch int
	markErr     error
	claimErr    error
}

func (r *fakeRepository) ClaimNextDispatch(_ context.Context, organization uuid.UUID) (*commands.Command, error) {
	r.claimedOrg = organization
	return r.command, r.claimErr
}
func (r *fakeRepository) MarkDispatched(_ context.Context, _, _ uuid.UUID) (commands.Command, error) {
	r.marked++
	if r.markErr != nil {
		return commands.Command{}, r.markErr
	}
	command := *r.command
	command.Status = commands.StatusDispatched
	return command, nil
}
func (r *fakeRepository) Fail(_ context.Context, _, _ uuid.UUID, _ commands.FailureCode) (commands.Command, error) {
	r.failed++
	return commands.Command{Status: commands.StatusFailed}, nil
}
func (r *fakeRepository) ExpireDueForOrganization(_ context.Context, organization uuid.UUID, limit int) ([]commands.Command, error) {
	r.expiredOrg, r.expiryBatch = organization, limit
	return nil, nil
}

type fakePublisher struct {
	ready   bool
	err     error
	calls   int
	topic   string
	payload []byte
}

func (p *fakePublisher) Ready() bool { return p.ready }
func (p *fakePublisher) Publish(_ context.Context, topic string, payload []byte) error {
	p.calls++
	p.topic = topic
	p.payload = append([]byte(nil), payload...)
	return p.err
}

func TestDispatchOncePublishesOnlyAfterDurableClaimAndMarksBrokerAcceptance(t *testing.T) {
	organizationID := uuid.New()
	deviceID := uuid.New()
	now := time.Date(2026, 10, 1, 3, 0, 0, 123456000, time.UTC)
	command := &commands.Command{
		ID: uuid.New(), DeviceID: deviceID, Type: commands.TypePing, Status: commands.StatusPending,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(commands.CommandLifetime), DispatchAttempts: 1,
	}
	repository := &fakeRepository{command: command}
	publisher := &fakePublisher{ready: true}
	runtime, err := New(repository, publisher, organizationID, "pulsegrid-dev", Config{}, clockFunc(func() time.Time { return now }), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if repository.claimedOrg != organizationID || publisher.calls != 1 || repository.marked != 1 {
		t.Fatalf("dispatch calls claim=%s publish=%d mark=%d", repository.claimedOrg, publisher.calls, repository.marked)
	}
	if publisher.topic != "pulsegrid/v1/tenants/pulsegrid-dev/devices/"+deviceID.String()+"/commands" {
		t.Fatalf("published topic = %q", publisher.topic)
	}
	decoded, err := mqttcontract.DecodeCommand(publisher.payload)
	if err != nil || decoded.CommandID != command.ID || decoded.Type != string(commands.TypePing) {
		t.Fatalf("published command = (%+v, %v)", decoded, err)
	}
}

func TestDispatchOncePreservesStateWhenPublishOutcomeIsUnknown(t *testing.T) {
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	command := &commands.Command{ID: uuid.New(), DeviceID: uuid.New(), Type: commands.TypePing, Status: commands.StatusPending,
		CreatedAt: createdAt, ExpiresAt: createdAt.Add(commands.CommandLifetime), DispatchAttempts: 2}
	repository := &fakeRepository{command: command}
	publisher := &fakePublisher{ready: true, err: errors.New("unknown publish result")}
	runtime, err := New(repository, publisher, uuid.New(), "pulsegrid-dev", Config{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("uncertain publish should be an observed retry condition: %v", err)
	}
	if publisher.calls != 1 || repository.marked != 0 || repository.failed != 0 || command.Status != commands.StatusPending {
		t.Fatalf("uncertain publish changed state: publish=%d mark=%d fail=%d status=%s", publisher.calls, repository.marked, repository.failed, command.Status)
	}
}

func TestDispatchOnceLeavesBrokerAcceptedCommandRetryableWhenStateWriteFails(t *testing.T) {
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	command := &commands.Command{ID: uuid.New(), DeviceID: uuid.New(), Type: commands.TypePing, Status: commands.StatusPending,
		CreatedAt: createdAt, ExpiresAt: createdAt.Add(commands.CommandLifetime), DispatchAttempts: 1}
	repository := &fakeRepository{command: command, markErr: errors.New("database write failed")}
	publisher := &fakePublisher{ready: true}
	runtime, err := New(repository, publisher, uuid.New(), "pulsegrid-dev", Config{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("broker-accepted write failure should remain retryable: %v", err)
	}
	if publisher.calls != 1 || repository.marked != 1 || repository.failed != 0 || command.Status != commands.StatusPending {
		t.Fatalf("post-publish failure changed state: publish=%d mark=%d fail=%d status=%s", publisher.calls, repository.marked, repository.failed, command.Status)
	}
}

func TestDispatchOnceSkipsWhenOfflineAndFailsInvalidDurableContract(t *testing.T) {
	command := &commands.Command{ID: uuid.New(), DeviceID: uuid.New(), Type: "UNSUPPORTED", Status: commands.StatusPending,
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(commands.CommandLifetime), DispatchAttempts: 1}
	repository := &fakeRepository{command: command}
	publisher := &fakePublisher{}
	runtime, err := New(repository, publisher, uuid.New(), "pulsegrid-dev", Config{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.DispatchOnce(context.Background()); err != nil || repository.claimedOrg != uuid.Nil {
		t.Fatalf("offline dispatch = (%v, %s)", err, repository.claimedOrg)
	}
	publisher.ready = true
	if err := runtime.DispatchOnce(context.Background()); err == nil || repository.failed != 1 || publisher.calls != 0 {
		t.Fatalf("invalid command result = (%v, fail=%d publish=%d)", err, repository.failed, publisher.calls)
	}
}

func TestExpiryUsesOrganizationAndBoundedBatch(t *testing.T) {
	organizationID := uuid.New()
	repository := &fakeRepository{}
	runtime, err := New(repository, &fakePublisher{}, organizationID, "pulsegrid-dev", Config{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.ExpireOnce(context.Background()); err != nil {
		t.Fatalf("ExpireOnce: %v", err)
	}
	if repository.expiredOrg != organizationID || repository.expiryBatch != commands.MaxExpireBatchSize {
		t.Fatalf("expiry scope = (%s, %d)", repository.expiredOrg, repository.expiryBatch)
	}
}
