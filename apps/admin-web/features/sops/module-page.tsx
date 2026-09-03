import { SopBuilder, SopLibrary, builderInitialFromVersion, isVersionFaithfullyEditable, sopSliceKey, toSopView } from "@/features/sops";
import type { SopCardView } from "@/features/sops";
import { getSop, isAuthRequiredError, listSops, requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";
import type { SopSliceDomain } from "./sop-derive";

// Shared server renderer for the per-module SOP pages (SOP split, maintainer decision 2026-08-18):
// /vaccination/sops, /counts/sops, and /feed/sops each mount this with their own page-contract key,
// slice, and base path. The retired top-level /sops authority screen is NOT a valid mount point.
//
// Lists real `/admin/sops` definitions scoped to the module's slice (sopSliceKey), then derives the
// card facets (domain / trigger / steps / gates) from real form_dsl + proof_policy. No mock rows.
//
// `?compose=1` (also the legacy `?new=1` deep link) swaps the library grid for the full-page SOP
// form builder at the SAME module route (no nested command route). `?edit=<sop_id>` reconstructs
// the builder from the SOP's latest version.
export async function renderSopModulePage(
  contractKey: string,
  slice: SopSliceDomain,
  basePath: string,
  searchParams: Promise<RouteSearchParams>,
) {
  const pageContractPromise = requireAdminWebPageContract(contractKey);
  const sp = await searchParams;
  if (sp.compose === "1" || sp.new === "1") {
    const editId = typeof sp.edit === "string" && sp.edit ? sp.edit : undefined;
    if (editId) {
      const [pageContract, detail] = await Promise.all([pageContractPromise, getSop(editId)]);
      if (detail.ok && detail.data.latest_version) {
        const version = detail.data.latest_version;
        const initial = builderInitialFromVersion(detail.data.sop.code, detail.data.sop.name, version.form_dsl, version.proof_policy);
        // If the version has rules/field-types this builder cannot round-trip, still show it (so the
        // author sees the SOP) but block save/publish — re-saving would silently drop that content.
        const editBlocked = !isVersionFaithfullyEditable(version.form_dsl);
        return <SopBuilder pageContract={pageContract} basePath={basePath} domain={slice} initial={initial} editSopId={editId} editBlocked={editBlocked} />;
      }
    }
    const pageContract = await pageContractPromise;
    return <SopBuilder pageContract={pageContract} basePath={basePath} domain={slice} />;
  }
  const [pageContract, listed] = await Promise.all([pageContractPromise, listSops({ limit: 200 })]);

  if (!listed.ok) {
    if (isAuthRequiredError(listed.error)) {
      return <SopLibrary sops={[]} authRequired pageContract={pageContract} basePath={basePath} />;
    }
    return <SopLibrary sops={[]} error={{ code: listed.error.code, message: listed.error.message }} pageContract={pageContract} basePath={basePath} />;
  }

  // Module scoping: each page lists only its own module's SOP codes. A SOP outside every module
  // slice (sopSliceKey "general") is not silently dropped into limbo — it belongs to no shipped
  // module yet and stays invisible until its module page exists, which is the honest state.
  const defs = listed.data.items.filter((def) => sopSliceKey(def.code, def.name) === slice);
  // Latest versions arrive EMBEDDED in the list response, populated by one batched backend query
  // (SOPListResponse.latest_versions, keyed by sop_id) — no per-SOP detail fan-out (C35-015).
  const latestVersions = listed.data.latest_versions ?? {};

  const sops: SopCardView[] = defs.map((def) => toSopView(def, latestVersions[def.sop_id] ?? null));

  return <SopLibrary sops={sops} pageContract={pageContract} basePath={basePath} />;
}
