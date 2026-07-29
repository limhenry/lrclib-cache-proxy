package ytmusic

import (
	"testing"
)

func TestParseDurationSeconds(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"4:45", 285},
		{"04:45", 285},
		{"1:02:15", 3735},
		{"invalid", 0},
	}

	for _, tt := range tests {
		got := parseDurationSeconds(tt.input)
		if got != tt.expected {
			t.Errorf("parseDurationSeconds(%q) = %d; want %d", tt.input, got, tt.expected)
		}
	}
}
