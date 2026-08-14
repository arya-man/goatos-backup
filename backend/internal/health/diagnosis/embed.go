package diagnosis

import (
	_ "embed"
	"fmt"
	"sync"
)

// The register ships as a committed file, embedded into the binary.
//
// Maintainer decision 2026-08-14 (E): this is the STARTING point, not the end
// state. The rule table must ultimately be editable by a vet without an
// engineer, and the machinery for that already exists -- /health/config runs
// draft -> publish -> retire with a write-log ledger and validate-or-reject at
// publish, and pins the published version at diagnosis. The register is a second
// authored artifact of exactly that shape.
//
// Load takes BYTES rather than a path precisely so that migration changes
// nothing in the engine: swapping this embed for a published row is a change of
// caller, not of algorithm.
//
//go:embed registers/adult-1.yaml
var adultRegisterYAML []byte

var (
	adultOnce sync.Once
	adultReg  *Register
	adultErr  error
)

// AdultRegister returns the embedded adult register, parsed once.
//
// It is validated on first load: a register that fails the structural checks
// (unknown vocabulary, an unanchored clause, a clause a higher tier makes
// unreachable) is refused rather than served, because every one of those defects
// silently changes which animals get diagnosed.
func AdultRegister() (*Register, error) {
	adultOnce.Do(func() {
		reg, err := Load(adultRegisterYAML)
		if err != nil {
			adultErr = fmt.Errorf("health: load embedded adult register: %w", err)
			return
		}
		if errs := reg.Validate(); len(errs) > 0 {
			adultErr = fmt.Errorf("health: embedded adult register failed validation: %v", errs[0])
			return
		}
		adultReg = reg
	})
	return adultReg, adultErr
}
