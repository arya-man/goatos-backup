import { SopBuilder, SopLibrary, builderInitialFromVersion, isVersionFaithfullyEditable, toSopView, type SopCardView } from "@/features/sops";
import { getSop, isAuthRequiredError, listSops, requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Admin / Data Ops / SOP Library (route reopened as the new SOP Library — not the old SOP/tasks UI).
// Lists real `/admin/sops` definitions, then fetches each SOP's latest version to derive the card
// facets (domain / trigger / steps / gates) from real form_dsl + proof_policy. No mock rows.
//
// `?compose=1` (also the legacy `?new=1` from the vaccination SOP quick-view "Create SOP" action)
// swaps the library grid for the full-page SOP form builder — a dedicated builder surface at the SAME
// top-level /sops authority route (no nested command route). No SOP list fetch is needed to compose.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [sp, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("sops")]);
  if (sp.compose === "1" || sp.new === "1") {
    // `?edit=<sop_id>` reconstructs the full builder state from the SOP's latest version (faithful edit,
    // saving publishes a NEW version); a missing/versionless SOP falls back to the create builder.
    const editId = typeof sp.edit === "string" && sp.edit ? sp.edit : undefined;
    if (editId) {
      const detail = await getSop(editId);
      if (detail.ok && detail.data.latest_version) {
        const version = detail.data.latest_version;
        const initial = builderInitialFromVersion(detail.data.sop.code, detail.data.sop.name, version.form_dsl, version.proof_policy);
        // If the version has rules/field-types this builder cannot round-trip, still show it (so the
        // author sees the SOP) but block save/publish — re-saving would silently drop that content.
        const editBlocked = !isVersionFaithfullyEditable(version.form_dsl);
        return <SopBuilder pageContract={pageContract} initial={initial} editSopId={editId} editBlocked={editBlocked} />;
      }
    }
    return <SopBuilder pageContract={pageContract} />;
  }
  const listed = await listSops({ limit: 200 });

  if (!listed.ok) {
    if (isAuthRequiredError(listed.error)) {
      return <SopLibrary sops={[]} authRequired pageContract={pageContract} />;
    }
    return <SopLibrary sops={[]} error={{ code: listed.error.code, message: listed.error.message }} pageContract={pageContract} />;
  }

  // SLICE WIDENED (maintainer request 2026-08-18): the library lists every real SOP definition —
  // vaccination plus the migration-seeded Counts (birth / death / shifting) and Feed
  // (distribution / packing / transport) documents. The backend-owned `domain_chips` option group
  // drives the filter bar; hiding a module again is a backend contract change, not a page filter.
  const defs = listed.data.items;
  // Latest versions arrive EMBEDDED in the list response, populated by one batched backend query
  // (SOPListResponse.latest_versions, keyed by sop_id). We no longer fan out one getSop detail call
  // per SOP — that list-then-N-details N+1 was O(SOP count) service calls per render (C35-015).
  const latestVersions = listed.data.latest_versions ?? {};

  const sops: SopCardView[] = defs.map((def) => toSopView(def, latestVersions[def.sop_id] ?? null));

  return <SopLibrary sops={sops} pageContract={pageContract} />;
}
