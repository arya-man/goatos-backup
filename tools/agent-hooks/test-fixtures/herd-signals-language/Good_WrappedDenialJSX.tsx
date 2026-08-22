// Good_WrappedDenialJSX.tsx — adversarial PASS fixture for
// check-herd-signals-language.mjs. Reproduces the REAL production copy from
// apps/admin-web/features/herd-signals/herd-signals-drawer.tsx verbatim in
// shape: a JSX text node whose denial sentence wraps across two lines, with
// the negation ("not the") at the END of the first line and the banned term
// ("body temperature") on the SECOND line. This is the exact case the
// guard's line-scoped check used to false-positive on — the negation and
// the term must be read as one sentence, not judged line-by-line. This file
// must never be wired into a real build target; it exists only for the
// guard's --self-test to load as a fixture.

export function TagTemperatureNote() {
  return (
    <p className="faint small" style={{ marginTop: 4 }}>
      Tag temperature is the temperature measured at the tag&apos;s own sensor housing — not the
      animal&apos;s body temperature.
    </p>
  );
}
