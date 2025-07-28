package main

import (
	"math"
	"testing"
)

func TestCalculateRatio(t *testing.T) {
	tests := []struct {
		name     string
		before   int64
		after    int64
		expected float64
	}{
		{
			name:     "normal case - after is double",
			before:   100,
			after:    200,
			expected: 2.0,
		},
		{
			name:     "normal case - after is half",
			before:   200,
			after:    100,
			expected: 0.5,
		},
		{
			name:     "no change",
			before:   100,
			after:    100,
			expected: 1.0,
		},
		{
			name:     "both zero",
			before:   0,
			after:    0,
			expected: 1.0,
		},
		{
			name:     "before is zero (new function)",
			before:   0,
			after:    100,
			expected: math.Inf(1),
		},
		{
			name:     "after is zero (removed function)",
			before:   100,
			after:    0,
			expected: 0.0,
		},
		{
			name:     "large values",
			before:   1000000,
			after:    2000000,
			expected: 2.0,
		},
		{
			name:     "small ratio",
			before:   1000,
			after:    1100,
			expected: 1.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateRatio(tt.before, tt.after)
			if math.IsInf(tt.expected, 1) {
				if !math.IsInf(result, 1) {
					t.Errorf("calculateRatio(%d, %d) = %f, want +Inf", tt.before, tt.after, result)
				}
			} else if math.Abs(result-tt.expected) > 0.001 {
				t.Errorf("calculateRatio(%d, %d) = %f, want %f", tt.before, tt.after, result, tt.expected)
			}
		})
	}
}

func TestFormatRatio(t *testing.T) {
	tests := []struct {
		name     string
		ratio    float64
		unit     string
		expected string
	}{
		{
			name:     "time-based - 2x slower",
			ratio:    2.0,
			unit:     "nanoseconds",
			expected: "2.0x slower",
		},
		{
			name:     "time-based - 2x faster",
			ratio:    0.5,
			unit:     "nanoseconds",
			expected: "2.0x faster",
		},
		{
			name:     "memory - 2x more",
			ratio:    2.0,
			unit:     "bytes",
			expected: "2.0x more",
		},
		{
			name:     "memory - 2x less",
			ratio:    0.5,
			unit:     "bytes",
			expected: "2.0x less",
		},
		{
			name:     "count - 3x more",
			ratio:    3.0,
			unit:     "count",
			expected: "3.0x more",
		},
		{
			name:     "count - 3x less",
			ratio:    0.333333,
			unit:     "count",
			expected: "3.0x less",
		},
		{
			name:     "no change",
			ratio:    1.0,
			unit:     "nanoseconds",
			expected: "unchanged",
		},
		{
			name:     "zero ratio (removed)",
			ratio:    0.0,
			unit:     "nanoseconds",
			expected: "removed",
		},
		{
			name:     "small improvement",
			ratio:    0.9,
			unit:     "nanoseconds",
			expected: "1.1x faster",
		},
		{
			name:     "small regression",
			ratio:    1.1,
			unit:     "nanoseconds",
			expected: "1.1x slower",
		},
		{
			name:     "infinite ratio (new function)",
			ratio:    math.Inf(1),
			unit:     "nanoseconds",
			expected: "new",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatRatio(tt.ratio, tt.unit)
			if result != tt.expected {
				t.Errorf("formatRatio(%f, %s) = %q, want %q", tt.ratio, tt.unit, result, tt.expected)
			}
		})
	}
}

func TestCalculateRatioEdgeCases(t *testing.T) {
	// Test potential overflow scenarios
	t.Run("large values no overflow", func(t *testing.T) {
		before := int64(math.MaxInt64 / 2)
		after := int64(math.MaxInt64 / 4)
		result := calculateRatio(before, after)
		expected := 0.5
		if math.Abs(result-expected) > 0.001 {
			t.Errorf("calculateRatio with large values failed: got %f, want %f", result, expected)
		}
	})

	// Test very small ratios
	t.Run("very small ratio", func(t *testing.T) {
		before := int64(1000000)
		after := int64(1)
		result := calculateRatio(before, after)
		expected := 0.000001
		if math.Abs(result-expected) > 0.0000001 {
			t.Errorf("calculateRatio with very small ratio failed: got %f, want %f", result, expected)
		}
	})

	// Test very large ratios
	t.Run("very large ratio", func(t *testing.T) {
		before := int64(1)
		after := int64(1000000)
		result := calculateRatio(before, after)
		expected := 1000000.0
		if math.Abs(result-expected) > 0.001 {
			t.Errorf("calculateRatio with very large ratio failed: got %f, want %f", result, expected)
		}
	})
}
