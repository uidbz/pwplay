package client

import (
	"math"
	"testing"
)

func TestVolumeCurve(t *testing.T) {
	for _, tc := range []struct{ level, gain float64 }{
		{0, 0}, {0.5, 0.125}, {0.8, 0.512}, {1, 1}, {1.5, 1}, {-1, 0},
	} {
		if got := LevelToGain(tc.level); math.Abs(got-tc.gain) > 1e-9 {
			t.Errorf("LevelToGain(%v) = %v, want %v", tc.level, got, tc.gain)
		}
	}
	for _, l := range []float64{0, 0.1, 0.33, 0.5, 0.9, 1} {
		if got := GainToLevel(LevelToGain(l)); math.Abs(got-l) > 1e-9 {
			t.Errorf("round trip %v -> %v", l, got)
		}
	}
	if got := GainToLevel(2); got != 1 {
		t.Errorf("GainToLevel(2) = %v, want 1", got)
	}
	if got := LevelPercent(StepGain(LevelToGain(0.5), 0.05)); got != 55 {
		t.Errorf("step from 50%% = %d%%, want 55%%", got)
	}
	if got := StepGain(1, 0.05); got != 1 {
		t.Errorf("StepGain above max = %v, want 1", got)
	}
}
