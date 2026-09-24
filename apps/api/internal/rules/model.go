// Package rules owns the MVP threshold-rule definition, evaluation, and
// immutable alert occurrence data.
package rules

import (
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
)

const (
	MetricTemperatureCelsius = "TEMPERATURE_CELSIUS"
	MaxRulesPerDevice        = 20
	DefaultAlertPageSize     = 50
	MaxAlertPageSize         = 100
)

type Comparator string

const (
	GreaterThan        Comparator = "GT"
	GreaterThanOrEqual Comparator = "GTE"
	LessThan           Comparator = "LT"
	LessThanOrEqual    Comparator = "LTE"
)

var (
	ErrInvalidInput     = errors.New("threshold rule input is invalid")
	ErrNotFound         = errors.New("threshold rule or alert was not found")
	ErrConflict         = errors.New("threshold rule conflicts with current state")
	ErrRuleLimitReached = errors.New("device threshold rule limit reached")
	ErrEvaluationFailed = errors.New("threshold rule evaluation failed")
	ErrAlertPersistence = errors.New("threshold alert persistence failed")
)

type Rule struct {
	ID               uuid.UUID
	DeviceID         uuid.UUID
	Metric           string
	Comparator       Comparator
	ThresholdCelsius float64
	Enabled          bool
	Revision         int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CreateRuleInput struct {
	DeviceID         uuid.UUID
	Comparator       Comparator
	ThresholdCelsius float64
	Enabled          bool
}

type UpdateRuleInput struct {
	ID               uuid.UUID
	ExpectedRevision int
	Comparator       Comparator
	ThresholdCelsius float64
	Enabled          bool
}

type Alert struct {
	ID                 uuid.UUID
	RuleID             uuid.UUID
	DeviceID           uuid.UUID
	MessageID          uuid.UUID
	ObservedAt         time.Time
	ReceivedAt         time.Time
	TemperatureCelsius float64
	Metric             string
	Comparator         Comparator
	ThresholdCelsius   float64
	CreatedAt          time.Time
}

// AlertCursor binds keyset pagination to the fixed principal and optional
// device filter that produced it.
type AlertCursor struct {
	CreatedAt      time.Time
	ID             uuid.UUID
	OrganizationID uuid.UUID
	DeviceFilter   *uuid.UUID
}

type AlertPage struct {
	Alerts     []Alert
	NextCursor *AlertCursor
}

func Compare(comparator Comparator, value, threshold float64) (bool, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || math.IsNaN(threshold) || math.IsInf(threshold, 0) {
		return false, ErrInvalidInput
	}
	switch comparator {
	case GreaterThan:
		return value > threshold, nil
	case GreaterThanOrEqual:
		return value >= threshold, nil
	case LessThan:
		return value < threshold, nil
	case LessThanOrEqual:
		return value <= threshold, nil
	default:
		return false, ErrInvalidInput
	}
}

func validateRuleInput(comparator Comparator, threshold float64) error {
	if _, err := Compare(comparator, threshold, threshold); err != nil {
		return err
	}
	return nil
}
