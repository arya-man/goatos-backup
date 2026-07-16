// Vaccination shed/execution endpoints serve the current operational view only.
// Preserve park scoping, but never forward the URL's historical top-bar as_of:
// the backend correctly rejects historical as_of, and the UI must not turn that into an empty board.
export function vaccinationCurrentViewScope(scope: { parkId?: string }): { parkId?: string } {
  return scope.parkId ? { parkId: scope.parkId } : {};
}
