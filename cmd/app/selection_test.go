package app

import (
	"reflect"
	"testing"
)

func TestParseDeviceSelection_BroadcastAll(t *testing.T) {
	tests := []struct {
		input string
		total int
		want  []int
	}{
		{"", 3, []int{0, 1, 2}},
		{"   ", 4, []int{0, 1, 2, 3}},
		{"a", 2, []int{0, 1}},
		{"A", 2, []int{0, 1}},
		{"all", 3, []int{0, 1, 2}},
		{"ALL", 5, []int{0, 1, 2, 3, 4}},
		{"", 1, []int{0}},
	}

	for _, tt := range tests {
		got, err := parseDeviceSelection(tt.input, tt.total)
		if err != nil {
			t.Fatalf("parseDeviceSelection(%q, %d) unexpected error: %v", tt.input, tt.total, err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseDeviceSelection(%q, %d) = %v, want %v", tt.input, tt.total, got, tt.want)
		}
	}
}

func TestParseDeviceSelection_SingleAndComma(t *testing.T) {
	tests := []struct {
		input string
		total int
		want  []int
	}{
		{"1", 3, []int{0}},
		{"2", 3, []int{1}},
		{"3", 3, []int{2}},
		{"1, 3", 3, []int{0, 2}},
		{"2, 1", 3, []int{0, 1}},
		{"1, 2, 3", 3, []int{0, 1, 2}},
		{"  1 ,  3  ", 4, []int{0, 2}},
		{"2, 2, 1", 3, []int{0, 1}}, // dedup
	}

	for _, tt := range tests {
		got, err := parseDeviceSelection(tt.input, tt.total)
		if err != nil {
			t.Fatalf("parseDeviceSelection(%q, %d) unexpected error: %v", tt.input, tt.total, err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseDeviceSelection(%q, %d) = %v, want %v", tt.input, tt.total, got, tt.want)
		}
	}
}

func TestParseDeviceSelection_Ranges(t *testing.T) {
	tests := []struct {
		input string
		total int
		want  []int
	}{
		{"1-3", 3, []int{0, 1, 2}},
		{"2-4", 5, []int{1, 2, 3}},
		{"1-1", 3, []int{0}},
		{"1-2, 4-5", 5, []int{0, 1, 3, 4}},
		{"1, 3-5, 2", 5, []int{0, 1, 2, 3, 4}},
		{"2-3, 1-2", 4, []int{0, 1, 2}}, // overlapping dedup
	}

	for _, tt := range tests {
		got, err := parseDeviceSelection(tt.input, tt.total)
		if err != nil {
			t.Fatalf("parseDeviceSelection(%q, %d) unexpected error: %v", tt.input, tt.total, err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseDeviceSelection(%q, %d) = %v, want %v", tt.input, tt.total, got, tt.want)
		}
	}
}

func TestParseDeviceSelection_Errors(t *testing.T) {
	errorCases := []struct {
		input string
		total int
	}{
		{"0", 3},        // out of bounds low
		{"4", 3},        // out of bounds high
		{"-1", 3},       // negative
		{"abc", 3},      // non-numeric
		{"1-4", 3},      // range out of bounds high
		{"0-2", 3},      // range out of bounds low
		{"3-1", 3},      // reversed range
		{"1-2-3", 3},    // malformed range
		{"1-", 3},       // trailing dash
		{"-2", 3},       // leading dash
		{",,,", 3},      // only commas
		{"1, foo", 3},   // invalid token
		{"", 0},         // zero total devices
	}

	for _, tt := range errorCases {
		got, err := parseDeviceSelection(tt.input, tt.total)
		if err == nil {
			t.Errorf("parseDeviceSelection(%q, %d) expected error, got %v", tt.input, tt.total, got)
		}
	}
}
