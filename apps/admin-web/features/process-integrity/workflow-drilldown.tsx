import Link from "next/link";
import { ArrowLeft, Ban, Syringe } from "lucide-react";
import { getVaccinationWorkflowDrilldown } from "@/lib/api/server";
import { copy, optionLabel, optionTone, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { type Tone } from "./process-integrity";
import { WorkflowStepper } from "./workflow-stepper";
import { Tag } from "@/components/ui-primitives";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { parseScope, scopeHref } from "@/lib/scope";
import { actionDriveLabel, actionWorkTitle } from "./work-board";

export async function VaccinationWorkflowDrilldownPage({
  rowId,
  searchParams,
  pageContract,
}: {
  rowId: string;
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
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
    from === "control-tower"
      ? copy(pageContract, "action.back_control")
      : from === "action-center"
        ? copy(pageContract, "action.back_action")
        : copy(pageContract, "action.back_workflows");
  const result = await getVaccinationWorkflowDrilldown(rowId);

  if (!result.ok) {
    return (
      <div className="screen on">
        <div className="phead">
          <div>
	            <div className="crumb">
	              <b>{copy(pageContract, "crumb")}</b>
	            </div>
	            <h1>{pageContract.title || copy(pageContract, "fallback.title")}</h1>
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
  const title = actionWorkTitle(pageContract, row);
  const drive = actionDriveLabel(pageContract, row);

  return (
    <div className="screen on">
      <div className="phead">
        <div>
	          <div className="crumb">
	            <b>{copy(pageContract, "crumb")}</b>
	          </div>
	          <h1>{title}</h1>
	          <div className="sub">{drive} · {row.park_name} · {row.shed_name} · {row.animal_stage} — {pageContract.subtitle}</div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Link href={backHref} className="btn">
          <ArrowLeft className="ic" style={{ width: 14 }} aria-hidden="true" /> {backLabel}
        </Link>
      </div>

      {/* Row summary — current computed state for this obligation. */}
      <div className="fchipsbar" style={{ marginBottom: 14 }}>
        <Syringe className="ic" style={{ width: 14, color: "var(--brand-d)" }} aria-hidden="true" />
	        <Tag tone={optionTone(pageContract, "work_state_filter_chips", row.work_state) as Tone}>{optionLabel(pageContract, "work_state_filter_chips", row.work_state)}</Tag>
	        <Tag tone={optionTone(pageContract, "severity_chips", row.severity) as Tone}>{optionLabel(pageContract, "severity_chips", row.severity)}</Tag>
	        <Tag tone={optionTone(pageContract, "sop_state_chips", row.sop_task_state) as Tone}>{optionLabel(pageContract, "sop_state_chips", row.sop_task_state)}</Tag>
	        <Tag tone={optionTone(pageContract, "proof_state_chips", row.proof_state) as Tone}>{optionLabel(pageContract, "proof_state_chips", row.proof_state)}</Tag>
	        <Tag tone={optionTone(pageContract, "verification_state_chips", row.verification_state) as Tone}>{optionLabel(pageContract, "verification_state_chips", row.verification_state)}</Tag>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">
	          {row.completed_count}/{row.expected_count} {copy(pageContract, "label.done")}
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
	          <h3>{copy(pageContract, "section.chain.title")}</h3>
	          <div className="sp" style={{ flex: 1 }} />
	          <span className="muted small">{copy(pageContract, "section.chain.next_prefix")} {row.next_action}</span>
        </div>
        <div className="bd">
          <WorkflowStepper nodes={nodes} />
        </div>
      </section>

      <div className="bd" style={{ paddingTop: 12, display: "flex", gap: 14, flexWrap: "wrap" }}>
        {row.goat_id ? (
          <Link href={`/goats/${encodeURIComponent(row.goat_id)}`} className="lk small">
	            {copy(pageContract, "action.goat_passport")} →
          </Link>
        ) : null}
        <Link href={scopeHref(`/vaccination/execution/sheds/${encodeURIComponent(row.shed_id)}`, scope)} className="lk small">
	          {copy(pageContract, "action.shed_execution")} →
        </Link>
        <Link href={scopeHref("/action-center", scope, {}, { ac_row: row.row_id })} className="lk small">
	          {copy(pageContract, "action.action_center")} →
        </Link>
        <Link href={scopeHref("/protocol-adherence", scope)} className="lk small">
	          {copy(pageContract, "action.protocol_adherence")} →
        </Link>
      </div>
    </div>
  );
}
