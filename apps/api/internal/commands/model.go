// Package commands owns command intent and its durable asynchronous lifecycle.
package commands

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultPageSize  = 20
	MaxPageSize      = 100
	MaxRequestIDSize = 64
	CommandLifetime  = 2 * time.Minute
)

type Type string

const (
	TypePing Type = "PING"
)

type Status string

const (
	StatusPending      Status = "PENDING"
	StatusDispatched   Status = "DISPATCHED"
	StatusAcknowledged Status = "ACKNOWLEDGED"
	StatusCompleted    Status = "COMPLETED"
	StatusFailed       Status = "FAILED"
	StatusTimedOut     Status = "TIMED_OUT"
)

type FailureCode string

const (
	FailureDeviceReported FailureCode = "DEVICE_REPORTED_FAILURE"
	FailureDelivery       FailureCode = "DELIVERY_FAILED"
)

type Event string

const (
	EventDispatch Event = "DISPATCH"
	EventAck      Event = "ACK"
	EventComplete Event = "COMPLETE"
	EventFail     Event = "FAIL"
	EventExpire   Event = "EXPIRE"
)

var (
	ErrInvalidInput      = errors.New("command input is invalid")
	ErrNotFound          = errors.New("command was not found")
	ErrConflict          = errors.New("command conflicts with current state")
	ErrInvalidTransition = errors.New("command transition is invalid")
	ErrTimedOut          = errors.New("command deadline has passed")
	ErrNotDue            = errors.New("command is not due to expire")
	ErrSchemaUnavailable = errors.New("command schema is unavailable")
)

type Command struct {
	ID             uuid.UUID
	DeviceID       uuid.UUID
	Type           Type
	Status         Status
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ExpiresAt      time.Time
	DispatchedAt   *time.Time
	AcknowledgedAt *time.Time
	TerminalAt     *time.Time
	FailureCode    *FailureCode
}

type CreateInput struct {
	DeviceID       uuid.UUID
	Type           Type
	IdempotencyKey uuid.UUID
	RequestID      string
}

type Cursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

type Page struct {
	Commands   []Command
	NextCursor *Cursor
}

// Apply returns an updated copy, whether the row changed, and a classified
// error. A deadline crossing is returned with the updated TIMED_OUT value so
// callers can commit timeout even when the triggering event arrived late.
func (command Command) Apply(event Event, now time.Time, failureCode FailureCode) (Command, bool, error) {
	now = normalizeTime(now)
	if !command.valid() || now.IsZero() {
		return command, false, ErrInvalidInput
	}
	if event == EventFail && failureCode != FailureDeviceReported && failureCode != FailureDelivery {
		return command, false, ErrInvalidInput
	}
	if command.Status == StatusTimedOut {
		return command, false, ErrTimedOut
	}
	if isTerminal(command.Status) {
		return applyTerminalDuplicate(command, event, failureCode)
	}

	if event == EventExpire {
		if now.Before(command.ExpiresAt) {
			return command, false, ErrNotDue
		}
		command.setTerminal(StatusTimedOut, now, nil)
		return command, true, nil
	}
	if event != EventDispatch && event != EventAck && event != EventComplete && event != EventFail {
		return command, false, ErrInvalidTransition
	}
	if !now.Before(command.ExpiresAt) {
		command.setTerminal(StatusTimedOut, now, nil)
		return command, true, ErrTimedOut
	}

	switch event {
	case EventDispatch:
		if command.Status != StatusPending {
			if command.Status == StatusDispatched || command.Status == StatusAcknowledged {
				return command, false, nil
			}
			return command, false, ErrInvalidTransition
		}
		command.Status = StatusDispatched
		command.DispatchedAt = timePointer(now)
		command.UpdatedAt = now
		return command, true, nil
	case EventAck:
		if command.Status == StatusAcknowledged {
			return command, false, nil
		}
		if command.Status != StatusPending && command.Status != StatusDispatched {
			return command, false, ErrInvalidTransition
		}
		command.recordReceipt(now)
		command.Status = StatusAcknowledged
		command.UpdatedAt = now
		return command, true, nil
	case EventComplete:
		if command.Status != StatusPending && command.Status != StatusDispatched && command.Status != StatusAcknowledged {
			return command, false, ErrInvalidTransition
		}
		command.recordReceipt(now)
		command.setTerminal(StatusCompleted, now, nil)
		return command, true, nil
	case EventFail:
		if command.Status != StatusPending && command.Status != StatusDispatched && command.Status != StatusAcknowledged {
			return command, false, ErrInvalidTransition
		}
		if failureCode == FailureDelivery && command.Status == StatusAcknowledged {
			return command, false, ErrInvalidTransition
		}
		if failureCode == FailureDeviceReported {
			command.recordReceipt(now)
		}
		command.setTerminal(StatusFailed, now, &failureCode)
		return command, true, nil
	default:
		return command, false, ErrInvalidTransition
	}
}

func (command Command) valid() bool {
	return command.ID != uuid.Nil && command.DeviceID != uuid.Nil &&
		command.Type == TypePing && command.CreatedAt.After(time.Time{}) &&
		command.ExpiresAt.After(command.CreatedAt) &&
		(command.Status == StatusPending || command.Status == StatusDispatched ||
			command.Status == StatusAcknowledged || command.Status == StatusCompleted ||
			command.Status == StatusFailed || command.Status == StatusTimedOut)
}

func applyTerminalDuplicate(command Command, event Event, failureCode FailureCode) (Command, bool, error) {
	switch {
	case event == EventAck:
		return command, false, nil
	case event == EventDispatch:
		return command, false, nil
	case event == EventComplete && command.Status == StatusCompleted:
		return command, false, nil
	case event == EventFail && command.Status == StatusFailed && command.FailureCode != nil && *command.FailureCode == failureCode:
		return command, false, nil
	case event == EventExpire:
		return command, false, nil
	case command.Status == StatusFailed || command.Status == StatusCompleted:
		return command, false, ErrConflict
	default:
		return command, false, ErrInvalidTransition
	}
}

func (command *Command) recordReceipt(at time.Time) {
	if command.DispatchedAt == nil {
		command.DispatchedAt = timePointer(at)
	}
	if command.AcknowledgedAt == nil {
		command.AcknowledgedAt = timePointer(at)
	}
}

func (command *Command) setTerminal(status Status, at time.Time, failureCode *FailureCode) {
	command.Status = status
	command.UpdatedAt = at
	command.TerminalAt = timePointer(at)
	command.FailureCode = failureCode
}

func isTerminal(status Status) bool {
	return status == StatusCompleted || status == StatusFailed || status == StatusTimedOut
}

func normalizeTime(value time.Time) time.Time {
	return value.UTC().Round(0).Truncate(time.Microsecond)
}

func timePointer(value time.Time) *time.Time {
	copy := value
	return &copy
}
