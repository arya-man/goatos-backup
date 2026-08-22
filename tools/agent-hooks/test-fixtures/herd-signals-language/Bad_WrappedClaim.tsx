// Bad_WrappedClaim.tsx — adversarial FAIL fixture for
// check-herd-signals-language.mjs. The INVERSE of the wrapped-denial
// fixtures: a genuine claim (no negation anywhere) split across lines must
// still be caught — fixing the false positive on wrapped denials must not
// weaken the check into missing wrapped claims. This file must never be
// wired into a real build target; it exists only for the guard's
// --self-test to load as a fixture.

export function BadClaimBodyTemp() {
  return (
    <p>
      The tag reports the animal's
      body temperature directly.
    </p>
  );
}

export function BadClaimEating() {
  return (
    <p>
      The dashboard shows this animal is
      eating right now.
    </p>
  );
}
