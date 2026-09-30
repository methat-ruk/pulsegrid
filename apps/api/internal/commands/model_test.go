package commands

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCommandTransitions(t *testing.T) {
	baseTime := time.Date(2026, 9, 29, 4, 0, 0, 123456789, time.UTC)
	tests := []struct {
		name          string
		events        []Event
		failureCode   FailureCode
		atDeadline    bool
		afterDeadline bool
		wantStatus    Status
		wantErr       error
		wantChanged   bool
	}{
		{name: "dispatch", events: []Event{EventDispatch}, wantStatus: StatusDispatched, wantChanged: true},
		{name: "ack from pending proves dispatch", events: []Event{EventAck}, wantStatus: StatusAcknowledged, wantChanged: true},
		{name: "complete from pending proves receipt", events: []Event{EventComplete}, wantStatus: StatusCompleted, wantChanged: true},
		{name: "device failure from pending proves receipt", events: []Event{EventFail}, failureCode: FailureDeviceReported, wantStatus: StatusFailed, wantChanged: true},
		{name: "delivery failure from pending", events: []Event{EventFail}, failureCode: FailureDelivery, wantStatus: StatusFailed, wantChanged: true},
		{name: "dispatch then ack", events: []Event{EventDispatch, EventAck}, wantStatus: StatusAcknowledged, wantChanged: true},
		{name: "result before ack", events: []Event{EventDispatch, EventComplete}, wantStatus: StatusCompleted, wantChanged: true},
		{name: "complete at deadline times out", events: []Event{EventComplete}, atDeadline: true, wantStatus: StatusTimedOut, wantErr: ErrTimedOut, wantChanged: true},
		{name: "ack after deadline times out", events: []Event{EventAck}, afterDeadline: true, wantStatus: StatusTimedOut, wantErr: ErrTimedOut, wantChanged: true},
		{name: "expire before deadline rejected", events: []Event{EventExpire}, wantStatus: StatusPending, wantErr: ErrNotDue},
		{name: "expire at deadline", events: []Event{EventExpire}, atDeadline: true, wantStatus: StatusTimedOut, wantChanged: true},
		{name: "unknown event rejected", events: []Event{"UNKNOWN"}, wantStatus: StatusPending, wantErr: ErrInvalidTransition},
		{name: "fail without code rejected", events: []Event{EventFail}, wantStatus: StatusPending, wantErr: ErrInvalidInput},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := testCommand(baseTime)
			var changed bool
			var err error
			for _, event := range test.events {
				now := baseTime.Add(time.Second)
				if test.atDeadline {
					now = command.ExpiresAt
				}
				if test.afterDeadline {
					now = command.ExpiresAt.Add(time.Microsecond)
				}
				command, changed, err = command.Apply(event, now, test.failureCode)
				if err != nil {
					break
				}
			}
			if command.Status != test.wantStatus {
				t.Fatalf("status = %s, want %s", command.Status, test.wantStatus)
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if changed != test.wantChanged {
				t.Fatalf("changed = %t, want %t", changed, test.wantChanged)
			}
			if command.CreatedAt.Nanosecond()%int(time.Microsecond) != 0 {
				t.Fatalf("createdAt precision = %s, want microsecond precision", command.CreatedAt)
			}
		})
	}
}

func TestCommandAckAndCompleteBeforeAtAndAfterDeadline(t *testing.T) {
	baseTime := time.Date(2026, 9, 29, 4, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		event      Event
		offset     time.Duration
		wantStatus Status
		wantErr    error
	}{
		{name: "ack before deadline", event: EventAck, offset: -time.Microsecond, wantStatus: StatusAcknowledged},
		{name: "ack at deadline", event: EventAck, wantStatus: StatusTimedOut, wantErr: ErrTimedOut},
		{name: "ack after deadline", event: EventAck, offset: time.Microsecond, wantStatus: StatusTimedOut, wantErr: ErrTimedOut},
		{name: "complete before deadline", event: EventComplete, offset: -time.Microsecond, wantStatus: StatusCompleted},
		{name: "complete at deadline", event: EventComplete, wantStatus: StatusTimedOut, wantErr: ErrTimedOut},
		{name: "complete after deadline", event: EventComplete, offset: time.Microsecond, wantStatus: StatusTimedOut, wantErr: ErrTimedOut},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := testCommand(baseTime)
			now := command.ExpiresAt.Add(test.offset)
			updated, changed, err := command.Apply(test.event, now, "")
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if !changed || updated.Status != test.wantStatus {
				t.Fatalf("transition = (%+v, %t), want status %s with a change", updated, changed, test.wantStatus)
			}

			if test.wantStatus == StatusTimedOut {
				if updated.TerminalAt == nil || !updated.TerminalAt.Equal(normalizeTime(now)) {
					t.Fatalf("timeout terminalAt = %v, want %s", updated.TerminalAt, normalizeTime(now))
				}
				return
			}
			if test.event == EventAck {
				if updated.DispatchedAt == nil || !updated.DispatchedAt.Equal(normalizeTime(now)) ||
					updated.AcknowledgedAt == nil || !updated.AcknowledgedAt.Equal(normalizeTime(now)) ||
					updated.TerminalAt != nil {
					t.Fatalf("ACK before deadline milestones = %+v", updated)
				}
				return
			}
			if updated.DispatchedAt == nil || !updated.DispatchedAt.Equal(normalizeTime(now)) ||
				updated.AcknowledgedAt == nil || !updated.AcknowledgedAt.Equal(normalizeTime(now)) ||
				updated.TerminalAt == nil || !updated.TerminalAt.Equal(normalizeTime(now)) {
				t.Fatalf("completion before deadline milestones = %+v", updated)
			}
		})
	}
}

func TestCommandDuplicateAndTerminalTransitions(t *testing.T) {
	createdAt := time.Date(2026, 9, 29, 4, 0, 0, 0, time.UTC)
	command := testCommand(createdAt)
	var changed bool
	var err error
	command, changed, err = command.Apply(EventDispatch, createdAt.Add(time.Second), "")
	if err != nil || !changed {
		t.Fatalf("dispatch = (%+v, %t, %v)", command, changed, err)
	}
	command, changed, err = command.Apply(EventDispatch, createdAt.Add(2*time.Second), "")
	if err != nil || changed || !command.DispatchedAt.Equal(createdAt.Add(time.Second)) {
		t.Fatalf("duplicate dispatch = (%+v, %t, %v)", command, changed, err)
	}
	command, changed, err = command.Apply(EventComplete, createdAt.Add(3*time.Second), "")
	if err != nil || !changed || command.Status != StatusCompleted {
		t.Fatalf("complete = (%+v, %t, %v)", command, changed, err)
	}
	command, changed, err = command.Apply(EventComplete, createdAt.Add(4*time.Second), "")
	if err != nil || changed || command.Status != StatusCompleted {
		t.Fatalf("duplicate complete = (%+v, %t, %v)", command, changed, err)
	}
	if _, _, err := command.Apply(EventFail, createdAt.Add(5*time.Second), FailureDeviceReported); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting terminal result = %v, want ErrConflict", err)
	}
	if _, _, err := command.Apply(EventAck, createdAt.Add(5*time.Second), ""); err != nil {
		t.Fatalf("late ACK after terminal result = %v, want no-op", err)
	}
}

func TestTimedOutCommandIgnoresLateResult(t *testing.T) {
	createdAt := time.Date(2026, 9, 29, 4, 0, 0, 0, time.UTC)
	command := testCommand(createdAt)
	command, _, err := command.Apply(EventExpire, command.ExpiresAt, "")
	if err != nil || command.Status != StatusTimedOut {
		t.Fatalf("expire = (%+v, %v)", command, err)
	}
	unchanged, changed, err := command.Apply(EventComplete, command.ExpiresAt.Add(time.Second), "")
	if !errors.Is(err, ErrTimedOut) || changed || unchanged.Status != StatusTimedOut {
		t.Fatalf("late result = (%+v, %t, %v), want unchanged timeout", unchanged, changed, err)
	}
}

func TestDeliveryFailureCannotFollowDeviceAcknowledgement(t *testing.T) {
	createdAt := time.Date(2026, 9, 29, 4, 0, 0, 0, time.UTC)
	command := testCommand(createdAt)
	command, _, err := command.Apply(EventAck, createdAt.Add(time.Second), "")
	if err != nil || command.Status != StatusAcknowledged {
		t.Fatalf("acknowledge = (%+v, %v)", command, err)
	}
	unchanged, changed, err := command.Apply(EventFail, createdAt.Add(2*time.Second), FailureDelivery)
	if !errors.Is(err, ErrInvalidTransition) || changed || unchanged.Status != StatusAcknowledged {
		t.Fatalf("delivery failure after ACK = (%+v, %t, %v), want rejected", unchanged, changed, err)
	}
}

func testCommand(createdAt time.Time) Command {
	createdAt = normalizeTime(createdAt)
	return Command{
		ID:        uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		DeviceID:  uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		Type:      TypePing,
		Status:    StatusPending,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
		ExpiresAt: createdAt.Add(CommandLifetime),
	}
}
