import Link from "next/link";
import { BookOpen, Plus, Upload } from "lucide-react";
import { getVaccinationOperations } from "@/lib/api/server";
import type { VaccinationOperationsResponse } from "@/lib/api/server";
import { type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope } from "@/lib/scope";
import { VaccinationExecutionSection } from "./execution-section";
import { VaccinationStatusMatrix } from "./status-matrix";
import { VaccinationCohortDetail } from "./cohort-detail";

// PHC · Vaccination — the operations floor, ported to the mock's single stacked screen:
//   header (SOP · Import sheet · New drive) → drive-mechanic band (Target → Group → Route → Execute)
//   → Vaccination status matrix → Per-cohort vaccination detail → shed-event execution section.
// No KPI strip, no tab switch — it is one mock-shaped screen. Command lenses (Control Tower / Action
// Center / Protocol Adherence / Workflows) stay top-level; this screen does not embed or shortcut them.
// Park scope comes from the shell top bar (?park); the page passes it to the read models.

// The drive mechanic, mock band: how a vaccination drive targets, groups, routes, and executes.
const DRIVE_STEPS: Array<{ step: string; title: string; detail: string }> = [
  { step: "Target", title: "Drive cohort", detail: "matching goats by cohort · age · park — never random individuals" },
  { step: "Group", title: "→ per-shed events", detail: "all matching goats grouped by shed" },
  { step: "Route", title: "→ shed owner", detail: "one batched notification per shed → its Manager, delegated to Asst" },
  { step: "Execute", title: "video per shed", detail: "FEFO dose consumed, posted on verify" },
];

export async function VaccinationOperationsPage({ searchParams }: { searchParams?: RouteSearchParams }) {
  const sp = searchParams ?? {};
  // Top-bar scope contract: park (backend-safe UUID) + as_of are honored by /vaccination/operations.
  const { parkId, asOf } = backendScope(parseScope(sp));

  const operations = await getVaccinationOperations({ parkId, asOf });
  const ops: VaccinationOperationsResponse | null = operations.ok ? operations.data : null;

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            PHC · <b>Vaccination</b>
          </div>
          <h1>Vaccination</h1>
          <div className="sub">
            The live vaccination floor — the drive mechanic, the cohort × vaccine status matrix, per-cohort
            detail, and the per-shed execution events. Exceptions and queues live in the top-level command screens.
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Link href="/sops" className="btn" title="Vaccination SOP policy">
          <BookOpen className="ic" aria-hidden="true" /> SOP
        </Link>
        <span
          className="btn"
          aria-disabled
          title="Bulk drive import isn't built yet — drives generate from a published vaccination protocol"
          style={{ opacity: 0.45, cursor: "not-allowed" }}
        >
          <Upload className="ic" aria-hidden="true" /> Import sheet
        </span>
        <Link href="/config?category=vaccination" className="btn p" title="Drives generate from a published vaccination protocol — author/publish it in Config">
          <Plus className="ic" aria-hidden="true" /> New drive
        </Link>
      </div>

      {!operations.ok ? (
        <div className="alert" style={{ marginBottom: 16 }}>
          <b>{operations.error.code ?? operations.error.kind}</b>&nbsp;{operations.error.message}
        </div>
      ) : null}

      {/* Drive mechanic — Target → Group → Route → Execute (mock band). */}
      <section className="card" style={{ marginBottom: 16 }}>
        <div className="bd">
          <div className="chain" tabIndex={0} role="group" aria-label="How a vaccination drive runs">
            {DRIVE_STEPS.map((c) => (
              <div className="cstep" key={c.step} style={{ cursor: "default" }}>
                <div className="s">{c.step}</div>
                <b>{c.title}</b>
                <div className="d">{c.detail}</div>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* Vaccination status matrix — cohort × vaccine protocol, from /vaccination/operations. */}
      <VaccinationStatusMatrix operations={ops} ok={operations.ok} />

      {/* Per-cohort vaccination detail — animals, age band, real last dose, next due, status. */}
      <VaccinationCohortDetail operations={ops} ok={operations.ok} />

      {/* Shed-event execution — the per-shed drive events (park/shed/owner/stock/status/next action).
          A NORMAL stacked section (mock "drive — shed events"), not a tab. Anchor id for deep links. */}
      <VaccinationExecutionSection searchParams={sp} />
    </div>
  );
}
