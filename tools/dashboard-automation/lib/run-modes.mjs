// Which prerequisites each runner mode is subject to. Pure, so the two pre-existing modes can be
// proved unchanged by a test rather than by reading the diff.
//
// Parity is never a gate for `write-journeys`: that lane restores everything it touched and proves
// the restore, so a stale STG-to-OCI parity receipt has nothing to say about whether a journey
// worked. Without this, a fully green write-journeys run would still report fail because a parity
// receipt was 26 hours old.
export const RUN_MODES = ["production-smoke", "post-main-certification", "write-journeys"];

/**
 * @param {string} mode
 * @param {boolean} certificationExtrasOptIn  GOATOS_DASHBOARD_CERTIFICATION_EXTRAS
 */
export function modeFlags(mode, certificationExtrasOptIn = false) {
  const isProductionSmoke = mode === "production-smoke";
  const isWriteJourneys = mode === "write-journeys";
  return {
    isProductionSmoke,
    isWriteJourneys,
    parityIsNeverAGate: isProductionSmoke || isWriteJourneys,
    runCertificationExtras: !isWriteJourneys && (!isProductionSmoke || certificationExtrasOptIn),
    dataTrustWhenUnchecked: isWriteJourneys ? "not_checked_write_clone" : isProductionSmoke ? "not_checked_read_only_smoke" : null
  };
}
