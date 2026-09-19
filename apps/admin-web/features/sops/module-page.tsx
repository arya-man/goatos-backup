import type { ReactNode } from "react";
import { SopBuilder, SopLibrary, builderInitialFromVersion, isVersionFaithfullyEditable, sopScopeKey, toSopView } from "@/features/sops";
import { FollowUpEditor } from "./followup-editor";
import { parseFollowUp } from "./followup-model";
import type { ReactNode } from "react";
import type { SopCardView } from "@/features/sops";
import { getSop, isAuthRequiredError, listSops, requireAdminWebPageContract } from "@/lib/api/server";
import type { AdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";
import type { SopScopeDomain } from "./sop-derive";
import { InspectionEditor } from "./inspection-editor";
import { publishedFromSearch } from "./published-href";
import { parseInspection, parseVendorForm } from "./inspection-model";
import { WeighingEditor } from "./weighing-editor";
import { parseWeighing } from "./weighing-model";
import { FeedEditor } from "./feed-editor";
import { parseFeed } from "./feed-model";
import { ShiftingEditor } from "./shifting-editor";
import { parseShifting } from "./shifting-model";
import { CaptureCardEditor } from "./capture-editor";
import { parseCaptureCard } from "./capture-model";

type renderSopExtraNodeFactory<t = AdminWebPageContract> = (pageContract: t, sp: RouteSearchParams) => Promise<ReactNode>;

// Shared server renderer for the per-module SOP pages (SOP split, maintainer decision 2026-08-18):
// /vaccination/sops, /counts/sops, and /feed/sops each mount this with their own page-contract key,
// slice, and base path. The retired top-level /sops authority screen is NOT a valid mount point.
//
// Lists real `/admin/sops` definitions scoped to the module's slice (sopScopeKey), then derives the
// card facets (domain / trigger / steps / gates) from real form_dsl + proof_policy. No mock rows.
//
// `?compose=1` (also the legacy `?new=1` deep link) swaps the library grid for the full-page SOP
// form builder at the SAME module route (no nested command route). `?edit=<sop_id>` reconstructs
// the builder from the SOP's latest version.
export async function renderSopModulePage(
  contractKey: string,
  slice: SopScopeDomain,
  basePath: string,
  searchParams: Promise<RouteSearchParams>,
  extraNodeFactory?: renderSopExtraNodeFactory,
) {
  const pageContractPromise = requireAdminWebPageContract(contractKey);
  const sp = await searchParams;
  const extraNode = extraNodeFactory ? await extraNodeFactory(await pageContractPromise, sp) : null;
  if (sp.compose === "1" || sp.new === "1") {
    const editId = typeof sp.edit === "string" && sp.edit ? sp.edit : undefined;
    if (editId) {
      const [pageContract, detail] = await Promise.all([pageContractPromise, getSop(editId)]);
      if (detail.ok && detail.data.latest_version) {
        // The editor opens the version IN FORCE (what the phone runs); an abandoned draft or a
        // retired version above it is never the base of the next publish.
        const version = detail.data.published_version ?? detail.data.latest_version;
        // HERD OPERATIONS CAPTURE CARD (maintainer decision 4, 2026-09-16): `&part=capture` opens
        // the Add birth / Add death form's SOP extras; the operator steps ride along verbatim.
        if (sp.part === "capture") {
          const capture = parseCaptureCard(detail.data.sop.code, version.form_dsl);
          if (capture) {
            const pageContract = await pageContractPromise;
            return (
              <CaptureCardEditor
                pageContract={pageContract}
                basePath={basePath}
                sopId={editId}
                sopName={detail.data.sop.name}
                sopCode={detail.data.sop.code}
                versionLabel={`${version.version_label} · ${version.status}`}
                initial={capture}
              />
            );
          }
        }
        // SOP-DRIVEN HERD OPERATIONS (maintainer decision 2026-09-13): a SOP that carries
        // operator steps (form_dsl.follow_up) is edited through the operator-steps editor. The
        // capture form it also carries is passed through verbatim on save (P1), so the old
        // "cannot round-trip these field types" block does not apply here.
        // SHIFTING SOP (maintainer decision 2026-09-16): the shifting SOP carries a `shifting`
        // cards section AND a dormant seeded follow_up track. The cards editor wins, checked
        // BEFORE the operator-steps editor; the follow_up track and the capture form are passed
        // through verbatim on save.
        const shifting = parseShifting(version.form_dsl);
        if (shifting) {
          const pageContract = await pageContractPromise;
          return (
            <ShiftingEditor
              pageContract={pageContract}
              basePath={basePath}
              sopId={editId}
              sopName={detail.data.sop.name}
              sopCode={detail.data.sop.code}
              versionLabel={`${version.version_label} · ${version.status}`}
              initial={shifting}
            />
          );
        }
        const followUp = parseFollowUp(version.form_dsl);
        if (followUp) {
          const pageContract = await pageContractPromise;
          return (
            <FollowUpEditor
              pageContract={pageContract}
              basePath={basePath}
              sopId={editId}
              sopName={detail.data.sop.name}
              sopCode={detail.data.sop.code}
              versionLabel={`${version.version_label} · ${version.status}`}
              initial={followUp}
              initialView={sp.view === "flow" ? "flow" : "list"}
            />
          );
        }
        // VENDOR FORM (2026-09-19): a SOP carrying a `vendor_form` document (the pages of
        // questions Add / Edit vendor asks) is edited through the same pages editor, without the
        // load form or media.
        const vendorForm = parseVendorForm(version.form_dsl);
        if (vendorForm) {
          const pageContract = await pageContractPromise;
          return (
            <InspectionEditor
              pageContract={pageContract}
              basePath={basePath}
              sopId={editId}
              sopName={detail.data.sop.name}
              sopCode={detail.data.sop.code}
              versionLabel={`${version.version_label} · ${version.status}`}
              initial={vendorForm}
              profile="vendor_form"
            />
          );
        }
        // PROCUREMENT SOP (maintainer decision 2026-09-14): a SOP carrying an `inspection`
        // document (pages of questions the phone runs) is edited through the inspection editor;
        // the load form it also carries is passed through verbatim on save.
        const inspection = parseInspection(version.form_dsl);
        if (inspection) {
          const pageContract = await pageContractPromise;
          return (
            <InspectionEditor
              pageContract={pageContract}
              basePath={basePath}
              sopId={editId}
              sopName={detail.data.sop.name}
              sopCode={detail.data.sop.code}
              versionLabel={`${version.version_label} · ${version.status}`}
              initial={inspection}
            />
          );
        }
        // FEED SOP (maintainer decision 2026-09-16): a feed.* SOP carrying a `feed` cards section
        // is edited through the feed cards editor; the capture form it also carries is passed
        // through verbatim on save.
        const feed = parseFeed(detail.data.sop.code, version.form_dsl);
        if (feed) {
          const pageContract = await pageContractPromise;
          return (
            <FeedEditor
              pageContract={pageContract}
              basePath={basePath}
              sopId={editId}
              sopName={detail.data.sop.name}
              sopCode={detail.data.sop.code}
              versionLabel={`${version.version_label} · ${version.status}`}
              initial={feed}
              initialView={sp.view === "flow" ? "flow" : "list"}
            />
          );
        }
        // WEIGHING SOP (maintainer decision 2026-09-15): a SOP carrying a `weighing` rules
        // section is edited through the rules editor; the capture form it also carries is
        // passed through verbatim on save.
        const weighing = parseWeighing(version.form_dsl);
        if (weighing) {
          const pageContract = await pageContractPromise;
          return (
            <WeighingEditor
              pageContract={pageContract}
              basePath={basePath}
              sopId={editId}
              sopName={detail.data.sop.name}
              sopCode={detail.data.sop.code}
              versionLabel={`${version.version_label} · ${version.status}`}
              initial={weighing}
              initialView={sp.view === "flow" ? "flow" : "list"}
            />
          );
        }
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
      return <SopLibrary sops={[]} authRequired pageContract={pageContract} basePath={basePath} extraNode={extraNode} />;
    }
    return <SopLibrary sops={[]} error={{ code: listed.error.code, message: listed.error.message }} pageContract={pageContract} basePath={basePath} extraNode={extraNode} />;
  }

  // Module scoping: each page lists only its own module's SOP codes. A SOP outside every module
  // slice (sopScopeKey "general") is not silently dropped into limbo — it belongs to no shipped
  // module yet and stays invisible until its module page exists, which is the honest state.
  const defs = listed.data.items.filter((def) => sopScopeKey(def.code, def.name) === slice);
  // Latest versions arrive EMBEDDED in the list response, populated by one batched backend query
  // (SOPListResponse.latest_versions, keyed by sop_id) — no per-SOP detail fan-out (C35-015).
  const latestVersions = listed.data.latest_versions ?? {};

  const sops: SopCardView[] = defs.map((def) => toSopView(def, latestVersions[def.sop_id] ?? null));

  // `?published=<sop_id>&v=<n>` is where an editor lands after Publish: the library says which
  // version just went live and lights up that card, so the change is visibly reflected instead
  // of a small note above an unchanged editor (maintainer report 2026-09-15).
  return <SopLibrary sops={sops} pageContract={pageContract} basePath={basePath} published={publishedFromSearch(sp)} extraNode={extraNode} />;
}
