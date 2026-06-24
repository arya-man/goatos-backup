import Link from "next/link";
import { ArrowLeft, Ban, Syringe } from "lucide-react";
import { getVaccinationWorkflowDrilldown } from "@/lib/api/server";
import type { WorkflowNode } from "@/lib/api/server";
import { PROOF_META, SEVERITY_META, SOP_META, VERIFICATION_META, WORK_STATE_META, type Tone } from "./process-integrity";
import { Tag } from "@/components/ui-primitives";
import { fmtDateTime } from "@/lib/format";

// Heuristic tone for a free-text node state — done/accepted/published → ok, rejected/blocked → dng, etc.
function nodeTone(state: string): Tone {
  const s = state.toLowerCase();
  if (/(reject|block|miss|fail|overdue)/.test(s)) return "dng";
  if (/(accept|complete|done|publish|verified|posted|generated)/.test(s)) return "ok";
  if (/(pending|await|progress|submitted|uploaded|open)/.test(s)) return "warn";
  if (/(not_started|not started|scheduled|n\/a|skipped)/.test(s)) return "mut";
  return "info";
}

function NodeRow({ node, last }: { node: WorkflowNode; last: boolean }) {
  return (
    <div style={{ display: "flex", gap: 12, alignItems: "flex-start" }}>
      <div style={{ display: "flex", flexDirection: "column", alignItems: "center", flexShrink: 0 }}>
        <span className="sw" style={{ width: 11, height: 11, borderRadius: 999, background: `var(--${nodeTone(node.state) === "ok" ? "brand" : nodeTone(node.state) === "dng" ? "danger" : nodeTone(node.state) === "warn" ? "amber" : "info"})` }} />
        {!last ? <span style={{ width: 2, flex: 1, minHeight: 26, background: "var(--line)", marginTop: 2 }} /> : null}
      </div>
      <div style={{ paddingBottom: 14, minWidth: 0, flex: 1 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
          <b style={{ fontSize: 13 }}>{node.label}</b>
          <Tag tone={nodeTone(node.state)}>{node.state}</Tag>
          {node.timestamp ? <span className="muted small">{fmtDateTime(node.timestamp)}</span> : null}
        </div>
        <div className="muted small" style={{ marginTop: 3, display: "flex", gap: 10, flexWrap: "wrap" }}>
          {node.actor ? <span>by {node.actor}</span> : null}
          {node.owner ? <span>owner: {node.owner}</span> : null}
          {node.evidence ? <span style={{ color: "var(--ok)" }}>{node.evidence}</span> : null}
        </div>
        {node.blocker ? (
          <div className="small" style={{ marginTop: 4, color: "var(--danger)", display: "flex", alignItems: "center", gap: 6 }}>
            <Ban className="ic" style={{ width: 13, flexShrink: 0 }} aria-hidden="true" />
            {node.blocker}
          </div>
        ) : null}
      </div>
    </div>
  );
}

export async function VaccinationWorkflowDrilldownPage({ rowId }: { rowId: string }) {
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
        <Link href="/workflows" className="btn">
          <ArrowLeft className="ic" style={{ width: 14 }} aria-hidden="true" /> Back to Workflows
        </Link>
      </div>
    );
  }

  const { row, nodes } = result.data;
  const title = row.drive_name ?? `${row.protocol_name} · ${row.dose_code}`;

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            <b>Workflows</b> · Vaccination
          </div>
          <h1>{title}</h1>
          <div className="sub">
            {row.park_name} · {row.shed_name} · {row.animal_stage} — config → obligation → drive → SOP → proof → verify →
            completion.
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Link href="/workflows" className="btn">
          <ArrowLeft className="ic" style={{ width: 14 }} aria-hidden="true" /> Workflows
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
          {nodes.length === 0 ? (
            <p className="muted small" style={{ margin: 0 }}>
              No workflow steps recorded yet for this obligation.
            </p>
          ) : (
            nodes.map((node, idx) => <NodeRow key={node.key} node={node} last={idx === nodes.length - 1} />)
          )}
        </div>
      </section>

      <div className="bd" style={{ paddingTop: 12, display: "flex", gap: 14, flexWrap: "wrap" }}>
        {row.goat_id ? (
          <Link href={`/goats/${encodeURIComponent(row.goat_id)}`} className="lk small">
            Goat passport →
          </Link>
        ) : null}
        <Link href={`/vaccination/execution/sheds/${encodeURIComponent(row.shed_id)}`} className="lk small">
          Shed execution detail →
        </Link>
        <Link href="/action-center" className="lk small">
          Action Center →
        </Link>
        <Link href="/protocol-adherence" className="lk small">
          Protocol Adherence →
        </Link>
      </div>
    </div>
  );
}
