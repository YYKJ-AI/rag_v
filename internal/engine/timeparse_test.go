package engine

import "testing"

func TestParseYMDRejectsNormalizedDates(t *testing.T) {
	for _, tt := range []struct {
		year, month, day string
		valid            bool
	}{
		{"2025", "02", "30", false},
		{"2025", "02", "29", false},
		{"2024", "02", "29", true},
	} {
		got := parseYMD(tt.year, tt.month, tt.day)
		if (got != nil) != tt.valid {
			t.Errorf("parseYMD(%s,%s,%s) = %v, want valid=%t", tt.year, tt.month, tt.day, got, tt.valid)
		}
	}
}
