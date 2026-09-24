package rules

import (
	"errors"
	"math"
	"testing"
)

func TestCompareThresholdOperatorsAndBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		comparator Comparator
		value      float64
		threshold  float64
		want       bool
	}{
		{name: "greater than below", comparator: GreaterThan, value: 9, threshold: 10},
		{name: "greater than equal", comparator: GreaterThan, value: 10, threshold: 10},
		{name: "greater than above", comparator: GreaterThan, value: 11, threshold: 10, want: true},
		{name: "greater than or equal below", comparator: GreaterThanOrEqual, value: 9, threshold: 10},
		{name: "greater than or equal boundary", comparator: GreaterThanOrEqual, value: 10, threshold: 10, want: true},
		{name: "greater than or equal above", comparator: GreaterThanOrEqual, value: 11, threshold: 10, want: true},
		{name: "less than below", comparator: LessThan, value: 9, threshold: 10, want: true},
		{name: "less than equal", comparator: LessThan, value: 10, threshold: 10},
		{name: "less than above", comparator: LessThan, value: 11, threshold: 10},
		{name: "less than or equal below", comparator: LessThanOrEqual, value: 9, threshold: 10, want: true},
		{name: "less than or equal boundary", comparator: LessThanOrEqual, value: 10, threshold: 10, want: true},
		{name: "less than or equal above", comparator: LessThanOrEqual, value: 11, threshold: 10},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Compare(test.comparator, test.value, test.threshold)
			if err != nil {
				t.Fatalf("Compare returned error: %v", err)
			}
			if got != test.want {
				t.Fatalf("Compare(%s, %v, %v) = %t, want %t", test.comparator, test.value, test.threshold, got, test.want)
			}
		})
	}
}

func TestCompareRejectsUnsupportedAndNonFiniteValues(t *testing.T) {
	for _, test := range []struct {
		name       string
		comparator Comparator
		value      float64
		threshold  float64
	}{
		{name: "unsupported comparator", comparator: "EQ", value: 1, threshold: 1},
		{name: "nan value", comparator: GreaterThan, value: math.NaN(), threshold: 1},
		{name: "infinite value", comparator: GreaterThan, value: math.Inf(1), threshold: 1},
		{name: "nan threshold", comparator: GreaterThan, value: 1, threshold: math.NaN()},
		{name: "infinite threshold", comparator: GreaterThan, value: 1, threshold: math.Inf(-1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Compare(test.comparator, test.value, test.threshold); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("Compare error = %v, want ErrInvalidInput", err)
			}
		})
	}
}
