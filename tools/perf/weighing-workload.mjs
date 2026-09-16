// Benchmark contract for the unfiltered Weights landing pages. Tests bind these
// defaults and section sets to the product sources; this is not product logic.
export const WEIGHING_WINDOW_FROM = "2026-08-03";
export const WEIGHING_LOOKBACK_DAYS = 400;
export const WEIGHING_DATES_NAME = "pr264_weighing_dates";
const windowQuery = "from={weighing_from}&to={weighing_to}&sex=male";
export const WEIGHING_WORKLOADS = Object.freeze({
  pr264_weighing_dates: "/weighing/weighing-dates?from={weighing_lookback_from}&to={weighing_today}&sex=male",
  pr264_weighing_shed_weights: `/weighing/shed-weights?${windowQuery}`,
  pr264_weighing_weight_demographics: `/weighing/weight-demographics?${windowQuery}&sections=composition,dimensions,gain_thresholds`,
  pr264_weighing_weight_demographics_dimensions_section: `/weighing/weight-demographics?${windowQuery}&sections=dimensions`,
  pr264_weighing_weight_demographics_origin_section: `/weighing/weight-demographics?${windowQuery}&sections=origin`,
  pr264_weighing_weight_demographics_shed_type_section: `/weighing/weight-demographics?${windowQuery}&sections=shed_type`,
  pr264_weighing_weight_demographics_weight_bands_section: `/weighing/weight-demographics?${windowQuery}&sections=weight_bands`,
  pr264_weighing_weight_demographics_weekly_gain_section: `/weighing/weight-demographics?${windowQuery}&sections=weekly_gain`,
  pr264_weighing_growth_weights_sections: `/weighing/leadership/growth?${windowQuery}&sections=headline,shed_leaderboard,losing_animals`,
  pr264_weighing_growth_general_sections: `/weighing/leadership/growth?${windowQuery}&sections=headline,shed_leaderboard,by_park`,
  pr264_weighing_growth_time_sections: `/weighing/leadership/growth?${windowQuery}&sections=weekly_gain`,
  pr264_growth_director_weights_sections: `/growth-director/weights?${windowQuery}&sections=road_to_sale,fair_fight`,
});

export function weighingWindow(startedAt, latest = "", copy = {}) {
  const today = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Kolkata", year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date(startedAt));
  const lookback = new Date(`${today}T00:00:00Z`);
  lookback.setUTCDate(lookback.getUTCDate() - WEIGHING_LOOKBACK_DAYS + 1);
  const day = /^\d{4}-\d{2}-\d{2}$/;
  const get = (key) => copy[`weights.window.${key}`]?.trim() ?? "";
  const earliest = day.test(get("earliest_date")) ? get("earliest_date") : "2026-08-01";
  let from = WEIGHING_WINDOW_FROM;
  if (get("default_from_mode") === "rolling_days") {
    const days = Number.parseInt(get("default_from_days"), 10);
    if (Number.isFinite(days) && days >= 1) from = new Date(Date.parse(`${today}T00:00:00Z`) - (days - 1) * 86400000).toISOString().slice(0, 10);
  } else if (get("default_from_mode") === "fixed_date" && day.test(get("default_from_date"))) from = get("default_from_date");
  if (from < earliest) from = earliest;
  if (from > today) from = today;
  const end = day.test(latest) && latest >= from && latest <= today ? latest : today;
  return {
    weighing_today: today,
    weighing_lookback_from: lookback.toISOString().slice(0, 10),
    weighing_from: from,
    weighing_to: end > today ? today : end,
  };
}

export function expandWeighingPath(path, window) {
  return path.replace(/\{(weighing_\w+)\}/g, (_, key) => {
    if (!window?.[key]) throw new Error(`Missing measured Weights landing window: ${key}`);
    return window[key];
  });
}

export function validateWeighingManifest(endpoints) {
  for (const [name, path] of Object.entries(WEIGHING_WORKLOADS)) {
    const matches = endpoints.filter((endpoint) => endpoint.name === name);
    if (matches.length !== 1 || matches[0].path !== path || (matches[0].method ?? "GET") !== "GET") {
      throw new Error(`${name} must measure the default Male/both-mode Weights landing request`);
    }
  }
  if (endpoints[0]?.name !== WEIGHING_DATES_NAME) throw new Error("Measure Weights dates before resolving the landing window");
}

// Consume the measured date read, never an unmeasured warmup or a manually
// declared short window. A moving dataset needs a new run, not mixed scopes.
export function weighingWindowFromResult(startedAt, result, copy = {}) {
  const samples = [...(result?.warmup_response_observations ?? []), ...(result?.response_observations ?? [])];
  if (!samples.length || samples.some((sample) => typeof sample.latest_weighing_date !== "string")) {
    throw new Error("Weights date observations must include latest_weighing_date");
  }
  const latest = samples[0].latest_weighing_date;
  if (samples.some((sample) => sample.latest_weighing_date !== latest)) throw new Error("Latest weighing date changed during measurement; rerun against one dataset");
  return weighingWindow(startedAt, latest, copy);
}

export function weighingEvidenceFailures(report) {
  const failures = [];
  let window;
  try {
    if (report.weighing_policy?.source !== "/admin-web/bootstrap" || !report.weighing_policy?.copy
      || JSON.stringify(report.weighing_policy) !== JSON.stringify(report.weighing_policy_end)) throw new Error("Weights page policy missing or changed during measurement");
    window = weighingWindowFromResult(report.started_at, report.results?.find(({name}) => name === WEIGHING_DATES_NAME), report.weighing_policy.copy);
  } catch (error) { return [error.message]; }
  for (const [name, template] of Object.entries(WEIGHING_WORKLOADS)) {
    const result = report.results?.find((item) => item.name === name);
    const expected = expandWeighingPath(template, window);
    if (!result || result.method !== "GET" || result.path !== expected) {
      failures.push(`${name} differs from the measured default Weights landing request`);
      continue;
    }
    for (const sample of [...(result.warmup_response_observations ?? []), ...(result.response_observations ?? [])]) {
      if (sample.request_path !== expected) failures.push(`${name} sample request differs from the default Weights landing request`);
    }
  }
  return failures;
}

// Only persist the window settings, not the authenticated bootstrap's other data.
export async function readWeighingPolicy({baseUrl, tenantId, bearerToken, cookie, timeoutMs = 30000, fetchImpl = fetch}) {
  const response = await fetchImpl(`${baseUrl}/admin-web/bootstrap`, {headers: {
    Accept: "application/json", "X-GoatOS-Tenant-ID": tenantId,
    ...(bearerToken ? {Authorization: `Bearer ${bearerToken}`} : {}), ...(cookie ? {Cookie: cookie} : {}),
  }, cache: "no-store", redirect: "error", signal: AbortSignal.timeout(timeoutMs)});
  if (!response.ok) throw new Error(`Weights page policy HTTP ${response.status}`);
  const body = await response.json();
  const copies = ["weighing-weights", "weighing-analytics"].map((id) => {
    const page = body.pages?.find((page) => page.route_id === id);
    if (!page?.copy) throw new Error(`Weights page policy missing ${id}`);
    return Object.fromEntries(["default_from_mode", "default_from_date", "default_from_days", "earliest_date"].map((key) => {
      const value = page.copy[`weights.window.${key}`] ?? "";
      if (typeof value !== "string") throw new Error("Invalid Weights page policy");
      return [`weights.window.${key}`, value];
    }));
  });
  if (JSON.stringify(copies[0]) !== JSON.stringify(copies[1])) throw new Error("Weights pages have different window policies");
  return {source: "/admin-web/bootstrap", copy: copies[0]};
}
