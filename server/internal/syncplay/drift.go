package syncplay

// Drift correction thresholds. A player within Tolerance of the shared
// position is left alone; up to SeekThreshold it is nudged by playing slightly
// faster or slower (inaudible with pitch correction, no visible jump); beyond
// that it seeks.
const (
	ToleranceMS     = 120
	SeekThresholdMS = 2500
	// nudgeSmall applies below nudgeLargeFromMS, nudgeLarge above it.
	nudgeSmall       = 0.05
	nudgeLarge       = 0.10
	nudgeLargeFromMS = 600
)

// Correction tells one member how to get back to the shared position.
type Correction struct {
	Mode string `json:"mode"` // "rate" or "seek"
	// Rate is the absolute playback rate to use for DurationMS (rate mode).
	Rate       float64 `json:"rate,omitempty"`
	DurationMS int64   `json:"durationMs,omitempty"`
	// PositionMS is the shared position at the message's serverTime (seek mode).
	PositionMS int64 `json:"positionMs,omitempty"`
	// DriftMS is reported minus shared position (positive = ahead).
	DriftMS int64 `json:"driftMs"`
}

// Drift decides the correction for a player that is driftMS ahead of (positive)
// or behind (negative) the shared position while the group plays at speed.
// It returns nil when the player is close enough.
func Drift(driftMS int64, speed float64) *Correction {
	if speed <= 0 {
		speed = 1
	}
	abs := driftMS
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs <= ToleranceMS:
		return nil
	case abs > SeekThresholdMS:
		return &Correction{Mode: "seek", DriftMS: driftMS}
	}
	nudge := nudgeSmall
	if abs > nudgeLargeFromMS {
		nudge = nudgeLarge
	}
	// Playing at speed*(1±nudge) for T wall-clock ms gains or loses speed*nudge*T
	// media ms against the group, so T = |drift| / (speed*nudge).
	rate := speed * (1 + nudge)
	if driftMS > 0 {
		rate = speed * (1 - nudge)
	}
	return &Correction{Mode: "rate", Rate: rate, DurationMS: int64(float64(abs) / (speed * nudge)), DriftMS: driftMS}
}
