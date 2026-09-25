package components

// Outcome is what an overlay reports after handling input. Overlays never
// close themselves; the model decides what is on screen.
type Outcome uint8

const (
	Continue Outcome = iota // keep the overlay open
	Submit                  // the user confirmed
	Cancel                  // the user dismissed the overlay
)
