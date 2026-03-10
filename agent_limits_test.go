package core

import "testing"

func TestReachedTurnLimit(t *testing.T) {
	tests := []struct {
		name     string
		turns    int
		maxTurns int
		want     bool
	}{
		{name: "below limit", turns: 2, maxTurns: 3, want: false},
		{name: "at limit", turns: 3, maxTurns: 3, want: true},
		{name: "zero unlimited", turns: 500, maxTurns: 0, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := reachedTurnLimit(tc.turns, tc.maxTurns)
			if got != tc.want {
				t.Fatalf("reachedTurnLimit(%d, %d) = %v, want %v", tc.turns, tc.maxTurns, got, tc.want)
			}
		})
	}
}
