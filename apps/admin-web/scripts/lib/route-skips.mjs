// GOATOS_SMOKE_ONLY_ROUTES names routes; some of them need a fixture id the run has to look up
// first (a goat, a seeded procurement load, a vaccination shed, a SOP by code). A lookup that
// comes back empty -- or fails with a backend error -- used to abort the whole module, so one
// missing fixture cost us every other route in the selection.
//
// It shouldn't. A route whose fixture is unavailable is skipped, with the reason, and the rest of
// the selection still runs. Only a selection where NOTHING can run is fatal, which is the case a
// caller genuinely needs to hear about.

/**
 * @param {object} input
 * @param {string[]} input.onlyRoutes      route names the caller asked for ([] = full sweep)
 * @param {string[]} input.builtRouteNames route names this run could actually build
 * @param {Map<string,string>|Record<string,string>} [input.skipReasons] route name -> why it dropped out
 * @returns {{ selectedNames: string[], skipped: {name: string, why: string}[] }}
 */
export function planRouteSelection({ onlyRoutes = [], builtRouteNames = [], skipReasons } = {}) {
  const built = new Set(builtRouteNames);
  const reasonFor = (name) => {
    if (skipReasons instanceof Map) return skipReasons.get(name);
    return skipReasons ? skipReasons[name] : undefined;
  };
  if (onlyRoutes.length === 0) {
    // A full sweep asks for everything, so anything with a recorded reason that did not get built
    // is a skip worth logging too -- that is how a 500 on one lookup stays visible in a lane log.
    const names = skipReasons instanceof Map ? [...skipReasons.keys()] : Object.keys(skipReasons ?? {});
    return {
      selectedNames: [...builtRouteNames],
      skipped: names.filter((name) => !built.has(name)).map((name) => ({ name, why: reasonFor(name) })),
    };
  }
  const selectedNames = onlyRoutes.filter((name) => built.has(name));
  const skipped = onlyRoutes
    .filter((name) => !built.has(name))
    .map((name) => ({ name, why: reasonFor(name) ?? "fixture id not available in this run" }));
  return { selectedNames, skipped };
}

/** The `route_skipped=<name>:<why>` line the lane logs are grepped for. */
export function routeSkippedLine({ name, why }) {
  return `route_skipped=${name}:${why}`;
}

/**
 * Nothing left to run is still fatal -- and the message names every route and why it went.
 * Returns null when the run may proceed.
 */
export function noRoutesLeftError({ onlyRoutes = [], selectedNames = [], skipped = [] } = {}) {
  if (selectedNames.length > 0) return null;
  if (onlyRoutes.length && skipped.length) {
    return `GOATOS_SMOKE_ONLY_ROUTES selected route(s) not available in this run: ${skipped.map((s) => `${s.name} (${s.why})`).join(", ")}`;
  }
  return "GOATOS_SMOKE_ONLY_ROUTES selected zero routes";
}
