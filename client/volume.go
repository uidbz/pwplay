package client

import "math"

// Volume levels vs gain.
//
// The server's volume is a linear gain factor (0 = silent, 1 = unity, up to
// 2). Loudness is perceived logarithmically, so a slider or percentage mapped
// straight onto gain is far too loud over most of its range: 20 % would be a
// gain of 0.2, only -14 dB. User-facing controls should instead show a
// *level* in 0…1 on the same cubic curve PipeWire, PulseAudio and pavucontrol
// use (gain = level³), so "50 %" here sounds like 50 % in any other app.

// LevelToGain converts a user-facing volume level (0…1, clamped) to the
// linear gain SetVolume expects.
func LevelToGain(level float64) float64 {
	if level <= 0 || math.IsNaN(level) {
		return 0
	}
	if level >= 1 {
		return 1
	}
	return level * level * level
}

// GainToLevel converts a linear gain (as reported in Status.Volume) to a
// user-facing level in 0…1. Gains above unity report as 1.
func GainToLevel(gain float64) float64 {
	if gain <= 0 || math.IsNaN(gain) {
		return 0
	}
	if gain >= 1 {
		return 1
	}
	return math.Cbrt(gain)
}

// StepGain moves a gain by delta on the level scale (e.g. +0.05 = "5 %
// louder") and returns the new gain, clamped to 0…1.
func StepGain(gain, delta float64) float64 {
	return LevelToGain(GainToLevel(gain) + delta)
}

// LevelPercent formats a gain as the rounded level percentage shown to users.
func LevelPercent(gain float64) int {
	return int(GainToLevel(gain)*100 + 0.5)
}
