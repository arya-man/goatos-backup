import { Ban, Check, Video } from "lucide-react";
import type { WorkflowNode } from "@/lib/api/server";
import { Tag } from "@/components/ui-primitives";
import { fmtDateTime } from "@/lib/format";
import type { Tone } from "./process-integrity";

// Heuristic tone for a free-text node state — done/accepted/published → ok, rejected/blocked → dng, etc.
export function nodeTone(state: string): Tone {
  const s = state.toLowerCase();
  if (/(reject|block|miss|fail|overdue)/.test(s)) return "dng";
  if (/(accept|complete|done|publish|verified|posted|generated)/.test(s)) return "ok";
  if (/(pending|await|progress|submitted|uploaded|open)/.test(s)) return "warn";
  if (/(not_started|not started|scheduled|n\/a|skipped)/.test(s)) return "mut";
  return "info";
}

function isDone(state: string): boolean {
  return /(accept|complete|done|publish|verified|posted|generated)/.test(state.toLowerCase());
}
function isBlocked(node: WorkflowNode): boolean {
  return !!node.blocker || /(reject|block|fail|overdue)/.test(node.state.toLowerCase());
}

// WorkflowStepper renders the canonical obligation chain as the mock's vertical stepper
// (numbered/checked circles, connector line, step description, proof + blocker pills). It is the
// SINGLE chain renderer shared by the Workflow record page and the Action Center drawer, so both
// surfaces match the mock instead of re-rolling plainer dot lists.
export function WorkflowStepper({ nodes }: { nodes: WorkflowNode[] }) {
  if (!nodes.length) {
    return (
      <p className="muted small" style={{ margin: 0 }}>
        No workflow steps recorded yet for this obligation.
      </p>
    );
  }
  // The current step is the first one that is not yet done (the mock highlights one ".cur" node).
  const firstPendingIdx = nodes.findIndex((n) => !isDone(n.state));
  return (
    <div className="stepper">
      {nodes.map((node, i) => {
        const done = isDone(node.state);
        const cur = i === firstPendingIdx;
        const blocked = isBlocked(node) && !done;
        const tone = nodeTone(node.state);
        return (
          <div key={node.key} className={`step${done ? " done" : ""}${cur ? " cur" : ""}`}>
            <div className="ln" />
            <div
              className="no"
              style={blocked ? { borderColor: "var(--danger)", color: "var(--danger)" } : undefined}
            >
              {done ? <Check className="ic" style={{ width: 14, strokeWidth: 2.4 }} aria-hidden="true" /> : i + 1}
            </div>
            <div className="ct">
              <b>{node.label}</b>
              <div className="d" style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
                <Tag tone={tone}>{node.state}</Tag>
                {node.actor ? <span>by {node.actor}</span> : null}
                {node.owner ? <span>owner: {node.owner}</span> : null}
                {node.timestamp ? <span>{fmtDateTime(node.timestamp)}</span> : null}
              </div>
              {node.evidence ? (
                <div className="vp">
                  <Tag tone="teal">
                    <Video className="ic" style={{ width: 12 }} aria-hidden="true" />
                    {node.evidence}
                  </Tag>
                </div>
              ) : null}
              {node.blocker ? (
                <div
                  className="vp small"
                  style={{ color: "var(--danger)", display: "flex", gap: 6, alignItems: "center" }}
                >
                  <Ban className="ic" style={{ width: 13, flexShrink: 0 }} aria-hidden="true" />
                  {node.blocker}
                </div>
              ) : null}
            </div>
          </div>
        );
      })}
    </div>
  );
}
