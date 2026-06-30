import { getSop, getVaccinationOperations, isAuthRequiredError, listSops } from "@/lib/api/server";
import type { VaccinationOperationsResponse } from "@/lib/api/server";
import { isVaccinationSop, toSopView, type SopCardView } from "@/features/sops";
import { type RouteSearchParams } from "@/lib/search-params";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { backendScope, parseScope } from "@/lib/scope";
import { VaccinationExecutionSection } from "./execution-section";
import { VaccinationStatusMatrix } from "./status-matrix";
import { VaccinationCohortDetail } from "./cohort-detail";
import { VaccinationSopButton } from "./sop-quick-view";
import { VaccinationHeaderActions } from "./vaccination-action-dialogs";

// Linked vaccination SOP for the header quick-view. Derived from the REAL /admin/sops data (same source as
// /sops), filtered to the vaccination slice and reduced to the primary (active preferred) SOP + its latest
// version. Errors/auth are surfaced in the modal rather than swallowed into a fake "no SOP" state.
type LinkedSop = { view: SopCardView | null; error?: { code?: string; message: string } | null; authRequired?: boolean };

async function loadLinkedVaccinationSop(): Promise<LinkedSop> {
  const listed = await listSops({ limit: 200 });
  if (!listed.ok) {
    if (isAuthRequiredError(listed.error)) return { view: null, authRequired: true };
    return { view: null, error: { code: listed.error.code, message: listed.error.message } };
  }
  const defs = listed.data.items.filter((d) => isVaccinationSop(d.code, d.name));
  if (defs.length === 0) return { view: null }; // no vaccination SOP authored yet (distinct from a load failure)
  const primary = defs.find((d) => d.status === "active") ?? defs[0];
  const detail = await getSop(primary.sop_id);
  if (!detail.ok) {
    // A detail-fetch failure must surface, not silently degrade to a version-less view that reads as success.
    if (isAuthRequiredError(detail.error)) return { view: null, authRequired: true };
    return { view: null, error: { code: detail.error.code, message: detail.error.message } };
  }
  return { view: toSopView(primary, detail.data.latest_version ?? null) };
}

// PHC · Vaccination — the operations floor, ported to the mock's single stacked screen:
//   header (SOP · Import sheet · New drive) → drive-mechanic band (Target → Group → Route → Execute)
//   → Vaccination status matrix → Per-cohort vaccination detail → shed-event execution section.
// No KPI strip, no tab switch — it is one mock-shaped screen. Command lenses (Control Tower / Action
// Center / Protocol Adherence / Workflows) stay top-level; this screen does not embed or shortcut them.
// Park scope comes from the shell top bar (?park); the page passes it to the read models.

export async function VaccinationOperationsPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  // Top-bar scope contract: park (backend-safe UUID) + as_of are honored by /vaccination/operations.
  const scope = parseScope(sp);
  const { parkId, asOf } = backendScope(scope);

  const [operations, linkedSop] = await Promise.all([
    getVaccinationOperations({ parkId, asOf }),
    loadLinkedVaccinationSop(),
  ]);
  const ops: VaccinationOperationsResponse | null = operations.ok ? operations.data : null;
  const driveSteps = optionGroup(pageContract, "drive_steps").map((step) => {
    const [title, detail] = (step.title || "").split("|");
    return { key: step.key, step: step.label, title, detail };
  });

  return (
    <div className="screen on">
      <div className="phead">
        <div>
	          <div className="crumb">
	            {copy(pageContract, "crumb")}
	          </div>
	          <h1>{pageContract.title}</h1>
	          <div className="sub">{pageContract.subtitle}</div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <VaccinationSopButton view={linkedSop.view} error={linkedSop.error} authRequired={linkedSop.authRequired} pageContract={pageContract} />
        <VaccinationHeaderActions scope={scope} pageContract={pageContract} />
      </div>

      {!operations.ok ? (
        <div className="alert" style={{ marginBottom: 16 }}>
          <b>{operations.error.code ?? operations.error.kind}</b>&nbsp;{operations.error.message}
        </div>
      ) : null}

      {/* Drive mechanic — Target → Group → Route → Execute (mock band). */}
      <section className="card" style={{ marginBottom: 16 }}>
        <div className="bd">
	          <div className="chain" tabIndex={0} role="group" aria-label={copy(pageContract, "section.drive_flow.aria")}>
            {driveSteps.map((c) => (
              <div className="cstep" key={c.key} style={{ cursor: "default" }}>
                <div className="s">{c.step}</div>
                <b>{c.title}</b>
                <div className="d">{c.detail}</div>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* Vaccination status matrix — cohort × vaccine protocol, from /vaccination/operations. */}
      <VaccinationStatusMatrix operations={ops} ok={operations.ok} scope={scope} searchParams={sp} pageContract={pageContract} />

      {/* Per-cohort vaccination detail — animals, age band, real last dose, next due, status. */}
      <VaccinationCohortDetail operations={ops} ok={operations.ok} scope={scope} searchParams={sp} pageContract={pageContract} />

      {/* Shed-event execution — the per-shed drive events (park/shed/owner/stock/status/next action).
          A NORMAL stacked section (mock "drive — shed events"), not a tab. Anchor id for deep links. */}
      <VaccinationExecutionSection searchParams={sp} pageContract={pageContract} />
    </div>
  );
}
