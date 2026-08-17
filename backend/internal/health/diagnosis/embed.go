package diagnosis

import (
	_ "embed"
	"fmt"
	"sync"
)

// The registers ship as committed files, embedded into the binary.
//
// Maintainer decision 2026-08-14 (E): this is the STARTING point, not the end
// state. The rule table must ultimately be editable by a vet without an
// engineer, and the machinery for that already exists -- /health/config runs
// draft -> publish -> retire with a write-log ledger and validate-or-reject at
// publish, and pins the published version at diagnosis. A register is a second
// authored artifact of exactly that shape.
//
// Load takes BYTES rather than a path precisely so that migration changes
// nothing in the engine: swapping these embeds for published rows is a change of
// caller, not of algorithm.

//go:embed registers/adult-1.yaml
var adultRegisterYAML []byte

//go:embed registers/kid-milk-7.yaml
var kidMilkRegisterYAML []byte

//go:embed registers/kid-weaning-1.yaml
var kidWeaningRegisterYAML []byte

//go:embed registers/kid-fattening-1.yaml
var kidFatteningRegisterYAML []byte

// Animal classes. One register serves each, and an animal is diagnosed against
// exactly one of them.
//
// These are GoatOS `class` values. The acceptance catalogs call the same field
// `klass` only because `class` is a Python keyword; the values are identical.
const (
	ClassAdult        = "adult"
	ClassKidMilk      = "kid_milk"
	ClassKidWeaning   = "kid_weaning"
	ClassKidFattening = "kid_fattening"
)

// classRegister binds one class to one embedded register at one pinned version.
//
// The version is pinned HERE as well as inside the file so that swapping a
// register for a newer authored revision cannot happen silently. A milk register
// republished as kid-milk-8 with a changed refusal ladder is a clinical change;
// it must break a build and be read, not load quietly because the filename still
// matched.
type classRegister struct {
	class   string
	version string
	yaml    []byte
}

var classRegisters = []classRegister{
	{ClassAdult, "adult-1", adultRegisterYAML},
	{ClassKidMilk, "kid-milk-7", kidMilkRegisterYAML},
	{ClassKidWeaning, "kid-weaning-1", kidWeaningRegisterYAML},
	{ClassKidFattening, "kid-fattening-1", kidFatteningRegisterYAML},
}

// Classes is every animal class this engine can diagnose, in the order the packs are
// declared above. Callers that must hold ALL registers (the diagnosis service resolves
// each one at startup so a malformed table fails the deploy, not a manager's first
// weaning kid) range over this rather than re-listing the ids and drifting from it.
var Classes = func() []string {
	out := make([]string, 0, len(classRegisters))
	for _, cr := range classRegisters {
		out = append(out, cr.class)
	}
	return out
}()

type loadedRegister struct {
	reg *Register
	err error
}

var (
	registersOnce sync.Once
	registerByCls map[string]loadedRegister
)

// loadAllRegisters parses every class register once. One class failing does not
// take the others down: a defect in the weaning table must not stop an adult
// from being diagnosed.
func loadAllRegisters() {
	registerByCls = make(map[string]loadedRegister, len(classRegisters))
	for _, cr := range classRegisters {
		reg, err := parseClassRegister(cr)
		registerByCls[cr.class] = loadedRegister{reg: reg, err: err}
	}
}

// parseClassRegister loads one register and proves it is the one this class
// should be served. Every failure here is a defect in a committed file.
func parseClassRegister(cr classRegister) (*Register, error) {
	reg, err := Load(cr.yaml)
	if err != nil {
		return nil, fmt.Errorf("health: load %s register: %w", cr.class, err)
	}
	if reg.Version != cr.version {
		return nil, fmt.Errorf(
			"health: %s register declares version %q but the binding pins %q -- a register revision is a clinical change and must be reviewed, not inferred from a filename",
			cr.class, reg.Version, cr.version)
	}
	// The register's own claim about which classes it serves is checked against
	// the class it is being bound to. This is the machine form of the spec's
	// loudest never: do not load adult YAML for a milk kid.
	if len(reg.AppliesClass) > 0 && !containsString(reg.AppliesClass, cr.class) {
		return nil, fmt.Errorf(
			"health: %s register applies_class=%v does not claim class %s",
			cr.class, reg.AppliesClass, cr.class)
	}
	if errs := reg.Validate(); len(errs) > 0 {
		return nil, fmt.Errorf("health: %s register failed validation: %w", cr.class, errs[0])
	}
	reg.boundClass = cr.class
	return reg, nil
}

// Evaluate diagnoses one animal against the register its class is served by.
//
// This is the entry point production uses. It exists so that choosing the
// register is not a decision each caller makes: the single most consequential
// rule in the whole spec is that a milk kid must never be diagnosed against the
// adult table, and a caller that picks its own register is one wrong map lookup
// away from doing exactly that.
func Evaluate(animal Animal, f Findings, ctx Context) (Proposal, error) {
	reg, err := RegisterFor(animal.class())
	if err != nil {
		return Proposal{}, err
	}
	return reg.Evaluate(animal, f, ctx), nil
}

// RegisterFor returns the register for one animal class, parsed and validated
// once.
//
// A register that fails its structural checks (unknown vocabulary, an unanchored
// clause, a clause a higher tier makes unreachable) is REFUSED rather than
// served, because every one of those defects silently changes which animals get
// diagnosed.
//
// An unknown class is an error, never a fallback to adult. Falling back would
// diagnose a kid against the adult table -- exactly the outcome the class split
// exists to prevent -- and it would do so silently.
func RegisterFor(class string) (*Register, error) {
	registersOnce.Do(loadAllRegisters)

	if class == "" {
		class = ClassAdult
	}
	entry, ok := registerByCls[class]
	if !ok {
		return nil, fmt.Errorf("health: no diagnosis register for animal class %q", class)
	}
	if entry.err != nil {
		return nil, entry.err
	}
	return entry.reg, nil
}

// AdultRegister is the adult register. Kept as a named accessor because the
// adult table is the one an operator-facing caller reaches for by default.
func AdultRegister() (*Register, error) { return RegisterFor(ClassAdult) }

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
