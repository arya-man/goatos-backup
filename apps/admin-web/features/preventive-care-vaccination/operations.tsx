import { getSop, isAuthRequiredError, listSops } from "@/lib/api/server";
import { isVaccinationSop, toSopView, type SopCardView } from "@/features/sops";
import { type RouteSearchParams } from "@/lib/search-params";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { parseScope } from "@/lib/scope";
import { VaccinationShedBoard } from "@/features/vaccination-sheds";
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

// Preventive Care (PC) · Vaccination — the SHED-WISE operations floor:
//   header (SOP · Protocol Rules) → drive-mechanic band (Target → Group → Route → Execute)
//   → shed-wise vaccination table (one row per shed, animal-level due/done, planned sessions, capacity,
//     merged status), which links to the shed detail at /vaccination/execution/sheds/{shed_id}.
// The old cohort/vaccine-wise status matrix + per-cohort detail + drive shed-event board are replaced by
// the shed-wise table per docs/runbooks/staging-vaccination-seed-preflight.md — no cohort/vaccine-wise
// rows appear on the main page. Command lenses (Control Tower / Action Center / Protocol Adherence /
// Workflows) stay top-level; this screen does not embed or shortcut them. Park scope comes from the shell
// top bar (?park).
export async function VaccinationOperationsPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const scope = parseScope(sp);

  const linkedSop = await loadLinkedVaccinationSop();
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

      {/* Shed-wise vaccination table — one row per shed, animal-level due/done, planned sessions, capacity,
          and merged status. Rows deep-link to the shed detail. This is the MAIN vaccination table. */}
      <VaccinationShedBoard searchParams={sp} pageContract={pageContract} />
    </div>
  );
}
