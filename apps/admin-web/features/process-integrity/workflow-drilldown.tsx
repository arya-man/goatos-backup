import Link from "next/link";
import { ArrowLeft, Ban, Syringe } from "lucide-react";
import { getVaccinationWorkflowDrilldown } from "@/lib/api/server";
import { PROOF_META, SEVERITY_META, SOP_META, VERIFICATION_META, WORK_STATE_META } from "./process-integrity";
import { WorkflowStepper } from "./workflow-stepper";
import { Tag } from "@/components/ui-primitives";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { parseScope, scopeHref } from "@/lib/scope";
import { actionDriveLabel, actionWorkTitle } from "./work-board";

export async function VaccinationWorkflowDrilldownPage({
  rowId,
  searchParams,
}: {
  rowId: string;
  searchParams?: RouteSearchParams;
}) {
  const sp = searchParams ?? {};
  const scope = parseScope(sp);
  const from = one(sp, "from");
  const backHref =
    from === "control-tower"
      ? scopeHref("/", scope, {}, { ct_alert: rowId })
      : from === "action-center"
        ? scopeHref("/action-center", scope, {}, { ac_row: rowId })
        : scopeHref("/workflows", scope);
  const backLabel =
    from === "control-tower" ? "Back to Control Tower" : from === "action-center" ? "Back to Action Center" : "Workflows";
  const result = await getVaccinationWorkflowDrilldown(rowId);

  if (!result.ok) {
    return (
      <div className="screen on">
        <div className="phead">
          <div>
            <div className="crumb">
              <b>Workflows</b> · Vaccination
            </div>
            <h1>Workflow drilldown</h1>
          </div>
        </div>
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </div>
        <Link href={backHref} className="btn">
          <ArrowLeft className="ic" style={{ width: 14 }} aria-hidden="true" /> {backLabel}
        </Link>
      </div>
    );
  }

  const { row, nodes } = result.data;
  const title = actionWorkTitle(row);
  const drive = actionDriveLabel(row);

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            <b>Workflows</b> · Vaccination
          </div>
          <h1>{title}</h1>
          <div className="sub">
            {drive} · {row.park_name} · {row.shed_name} · {row.animal_stage} — config → obligation → drive → SOP → proof → verify →
            completion.
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Link href={backHref} className="btn">
          <ArrowLeft className="ic" style={{ width: 14 }} aria-hidden="true" /> {backLabel}
        </Link>
      </div>

      {/* Row summary — current computed state for this obligation. */}
      <div className="fchipsbar" style={{ marginBottom: 14 }}>
        <Syringe className="ic" style={{ width: 14, color: "var(--brand-d)" }} aria-hidden="true" />
        <Tag tone={WORK_STATE_META[row.work_state].tone}>{WORK_STATE_META[row.work_state].label}</Tag>
        <Tag tone={SEVERITY_META[row.severity].tone}>{SEVERITY_META[row.severity].label}</Tag>
        <Tag tone={SOP_META[row.sop_task_state].tone}>{SOP_META[row.sop_task_state].label}</Tag>
        <Tag tone={PROOF_META[row.proof_state].tone}>{PROOF_META[row.proof_state].label}</Tag>
        <Tag tone={VERIFICATION_META[row.verification_state].tone}>{VERIFICATION_META[row.verification_state].label}</Tag>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">
          {row.completed_count}/{row.expected_count} done
        </span>
      </div>

      {row.blocker_reason ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <Ban className="ic" aria-hidden="true" />
          <div>{row.blocker_reason}</div>
        </div>
      ) : null}

      <section className="card">
        <div className="hd">
          <Syringe className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
          <h3>Workflow chain</h3>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">next: {row.next_action}</span>
        </div>
        <div className="bd">
          <WorkflowStepper nodes={nodes} />
        </div>
      </section>

      <div className="bd" style={{ paddingTop: 12, display: "flex", gap: 14, flexWrap: "wrap" }}>
        {row.goat_id ? (
          <Link href={`/goats/${encodeURIComponent(row.goat_id)}`} className="lk small">
            Goat passport →
          </Link>
        ) : null}
        <Link href={scopeHref(`/vaccination/execution/sheds/${encodeURIComponent(row.shed_id)}`, scope)} className="lk small">
          Shed execution detail →
        </Link>
        <Link href={scopeHref("/action-center", scope, {}, { ac_row: row.row_id })} className="lk small">
          Action Center →
        </Link>
        <Link href={scopeHref("/protocol-adherence", scope)} className="lk small">
          Protocol Adherence →
        </Link>
      </div>
    </div>
  );
}
