import { WeighingWeightsAnalyticsPage, landingWindow, WINDOW_FROM_PARAM, WINDOW_TO_PARAM } from "@/features/weighing";
import { requireAdminWebPageContract } from "@/lib/api/server";
import { todayIso } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { redirect } from "next/navigation";

export const dynamic = "force-dynamic";

const PAGE_PATH = "/weighing/analytics";

function weighingModeFilter(raw: string | undefined): string {
  return raw === "individual_animal" || raw === "per_shed_partition" ? raw : "all";
}

function hrefWithWindow(params: RouteSearchParams, from: string, to: string): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (Array.isArray(value)) value.forEach((item) => next.append(key, item));
    else if (value) next.set(key, value);
  }
  next.set(WINDOW_FROM_PARAM, from);
  next.set(WINDOW_TO_PARAM, to);
  return `${PAGE_PATH}?${next.toString()}`;
}

async function redirectToCanonicalWindow(params: RouteSearchParams): Promise<void> {
  if (one(params, WINDOW_FROM_PARAM) || one(params, WINDOW_TO_PARAM)) return;

  const rawSex = one(params, "sex");
  const sexFilter = rawSex === "female" ? "female" : rawSex === "all" ? "" : "male";
  const rawOrigin = one(params, "origin");
  const originFilter = rawOrigin === "farm_born" || rawOrigin === "purchased" ? rawOrigin : "";
  const modeFilter = weighingModeFilter(one(params, "weighing"));
  const weighingCategoryFilter = modeFilter !== "all" ? modeFilter : "";
  const window = await landingWindow(
    params,
    todayIso(),
    one(params, "park") ?? "",
    sexFilter,
    originFilter,
    weighingCategoryFilter,
  );
  redirect(hrefWithWindow(params, window.from, window.to));
}

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const params = await searchParams;
  await redirectToCanonicalWindow(params);
  // serial-await: allow page contract is deliberately fetched only after the no-window request has redirected to its canonical dated URL.
  const pageContract = await requireAdminWebPageContract("weighing-analytics");
  return (
    <WeighingWeightsAnalyticsPage
      searchParams={params}
      pageContract={pageContract}
    />
  );
}
