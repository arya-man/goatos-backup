// Bad_ClaimShapedCopy.go — adversarial FAIL fixture for
// check-herd-signals-language.mjs. Every line below is a claim-shaped
// assertion the tag hardware cannot support, or a mislabeled field. This
// file must never be wired into a real build target; it exists only for the
// guard's --self-test to load as a fixture.

package fixture

type AnimalStatus struct {
	// A field name that directly encodes a banned behavior claim.
	IsEating bool `json:"is_eating"`

	// A mislabeled temperature field: the tag has no animal-contact sensor.
	BodyTemperature float64 `json:"body_temperature_c"`
}

func summarize() string {
	// A claim-shaped sentence: asserts the tag detects a named behavior.
	label := "Eating detected"
	return label
}

func classify(delta int64) string {
	if delta > 100 {
		// Another claim-shaped assertion: "animal is running".
		return "animal is running"
	}
	return "unknown"
}
