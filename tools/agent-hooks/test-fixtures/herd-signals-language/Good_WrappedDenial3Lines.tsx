// Good_WrappedDenial3Lines.tsx — adversarial PASS fixture for
// check-herd-signals-language.mjs. A denial sentence wrapped across THREE
// lines (negation two lines before the banned terms), well within
// WINDOW_BEFORE. Must never be wired into a real build target; exists only
// for the guard's --self-test.

export function BoundaryNote() {
  return (
    <p>
      This module does
      not
      detect eating, rumination, sitting, or standing.
    </p>
  );
}
