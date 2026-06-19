"use client";

import { useRef, useState } from "react";
import type { Dispatch, PointerEvent as ReactPointerEvent } from "react";
import type { BuilderNode, BuilderState } from "../model";
import type { Action } from "../state";
import { FLOW_PATTERNS, NODE_PALETTE, nodeVisualClass, suggestLinkLabel } from "../templates";

const CANVAS_W = 748;
const CANVAS_H = 435;
const NODE_W = 148;

export function FlowTab({ state, dispatch, lit }: { state: BuilderState; dispatch: Dispatch<Action>; lit: Set<string> }) {
  const [nodeLabel, setNodeLabel] = useState("Finance acknowledgement");
  const [nodeRole, setNodeRole] = useState(NODE_PALETTE[1].key);
  const [nodeAfter, setNodeAfter] = useState(state.nodes[0]?.id ?? "");

  return (
    <div>
      <div className="flow-note">
        <h3>Task Flow decides who handles the task after it is created.</h3>
        <p>Pick a flow pattern first. Then adjust steps only if the SOP needs a special approval, proof review, rework, or escalation path.</p>
        <div className="info-line">
          <span className="i">i</span>
          <span>Form Logic controls fields inside the form. Task Flow controls handoffs after submit.</span>
        </div>
      </div>

      <div className="panel">
        <h3>Choose Task Flow Pattern</h3>
        <div className="desc">Most SOPs should use one of these simple patterns instead of drawing a workflow from scratch.</div>
        <div className="pattern-grid">
          {Object.entries(FLOW_PATTERNS).map(([key, p]) => (
            <button key={key} type="button" className={`pattern-card${state.flowPattern === key ? " active" : ""}`} onClick={() => dispatch({ kind: "setFlowPattern", pattern: key })}>
              <div className="name">{p.name}</div>
              <div className="copy">{p.copy}</div>
            </button>
          ))}
        </div>
      </div>

      <div className="panel">
        <h3>Task Flow Summary</h3>
        <div className="desc">The layman view. Each card is a step; each chip between cards says why the task moves forward.</div>
        <div className="flow-summary">
          {state.links.map(([from, to, label], i) => {
            const fromNode = state.nodes.find((n) => n.id === from);
            const toNode = state.nodes.find((n) => n.id === to);
            return (
              <div key={i} className="flow-route">
                <div className="flow-step"><div className="step-name">{fromNode?.label ?? from}</div><div className="step-role">{fromNode?.role ?? "step"}</div></div>
                <div className="flow-arrow"><span>{label || "next"}</span></div>
                <div className="flow-step"><div className="step-name">{toNode?.label ?? to}</div><div className="step-role">{toNode?.role ?? "step"}</div></div>
              </div>
            );
          })}
        </div>
      </div>

      <div className="panel">
        <h3>Add Special Step</h3>
        <div className="desc">Use this only when the selected pattern is not enough.</div>
        <div className="builder-form">
          <div className="inputgroup wide">
            <label>Step name</label>
            <input value={nodeLabel} onChange={(e) => setNodeLabel(e.target.value)} />
          </div>
          <div className="inputgroup">
            <label>Step type</label>
            <select value={nodeRole} onChange={(e) => setNodeRole(e.target.value)}>
              {NODE_PALETTE.map((p) => <option key={p.key} value={p.key}>{p.label}</option>)}
            </select>
          </div>
          <div className="inputgroup">
            <label>Place after</label>
            <select value={nodeAfter} onChange={(e) => setNodeAfter(e.target.value)}>
              {state.nodes.map((n) => <option key={n.id} value={n.id}>{n.label}</option>)}
            </select>
          </div>
          <button className="btn small" type="button" onClick={() => dispatch({ kind: "addNode", roleKey: nodeRole, label: nodeLabel, after: nodeAfter || state.nodes[0]?.id || "start" })}>Add and link step</button>
        </div>
        <div className="canvas-title" style={{ marginTop: 14 }}>
          <h4>Advanced step palette</h4>
          <span className="hint">Click a block to add it after the selected step.</span>
        </div>
        <div className="palette" style={{ marginTop: 8 }}>
          {NODE_PALETTE.map((p) => (
            <div key={p.key} className="chip" onClick={() => dispatch({ kind: "addNode", roleKey: p.key, label: p.label, after: state.selectedNodeId ?? state.nodes[0]?.id ?? "start" })}>
              <span className="ic">{p.icon}</span><span>{p.label}</span>
            </div>
          ))}
        </div>
      </div>

      <NodeEditor state={state} dispatch={dispatch} />
      <LinkEditor key={state.selectedLinkIndex ?? "new"} state={state} dispatch={dispatch} />

      <div className="panel">
        <h3>Advanced Canvas</h3>
        <div className="desc">Drag a node to reposition; connectors follow. Drag from a + handle to another box to connect. Click a line to edit it. Lit nodes = current preview path.</div>
        <FlowCanvas state={state} dispatch={dispatch} lit={lit} />
      </div>
    </div>
  );
}

function NodeEditor({ state, dispatch }: { state: BuilderState; dispatch: Dispatch<Action> }) {
  const node = state.nodes.find((n) => n.id === state.selectedNodeId) ?? null;
  return (
    <div className="panel">
      <h3>Edit Selected Step</h3>
      <div className="desc">{node ? `Editing ${node.label}. Drag it on the canvas or change settings here.` : "Select a workflow box to edit it, or add one from the palette."}</div>
      <div className="builder-form">
        <div className="inputgroup wide">
          <label>Step name</label>
          <input disabled={!node} value={node?.label ?? ""} onChange={(e) => node && dispatch({ kind: "updateNode", id: node.id, patch: { label: e.target.value } })} />
        </div>
        <div className="inputgroup">
          <label>Step type</label>
          <select
            disabled={!node}
            value={NODE_PALETTE.find((p) => p.type === node?.type)?.key ?? "operator"}
            onChange={(e) => {
              if (!node) return;
              const spec = NODE_PALETTE.find((p) => p.key === e.target.value);
              if (spec) dispatch({ kind: "updateNode", id: node.id, patch: { type: spec.type, role: spec.role } });
            }}
          >
            {NODE_PALETTE.map((p) => <option key={p.key} value={p.key}>{p.label}</option>)}
          </select>
        </div>
        <button className="btn small" type="button" disabled={!node || node.id === "start"} onClick={() => node && dispatch({ kind: "deleteNode", id: node.id })}>Delete step</button>
      </div>
    </div>
  );
}

function LinkEditor({ state, dispatch }: { state: BuilderState; dispatch: Dispatch<Action> }) {
  // Remounted (via key) whenever selectedLinkIndex changes, so initial state
  // is seeded from the selected link without a synchronous setState effect.
  const selected = state.selectedLinkIndex != null ? state.links[state.selectedLinkIndex] : null;
  const [from, setFrom] = useState(selected?.[0] ?? state.nodes[0]?.id ?? "");
  const [to, setTo] = useState(selected?.[1] ?? state.nodes[1]?.id ?? "");
  const [label, setLabel] = useState(selected?.[2] ?? "");
  const [about, setAbout] = useState(selected?.[3] ?? "");

  const save = () => {
    if (!from || !to || from === to) return;
    dispatch({ kind: "saveLink", from, to, label: label.trim(), about: about.trim(), replaceIndex: state.selectedLinkIndex });
  };

  return (
    <div className="panel">
      <h3>Edit Handoffs</h3>
      <div className="desc">A handoff is the reason a task moves from one step to another: approved, rejected, proof captured, blocked, or accepted.</div>
      <div className="builder-form">
        <div className="inputgroup">
          <label>From</label>
          <select value={from} onChange={(e) => setFrom(e.target.value)}>{state.nodes.map((n) => <option key={n.id} value={n.id}>{n.label}</option>)}</select>
        </div>
        <div className="inputgroup">
          <label>To</label>
          <select value={to} onChange={(e) => setTo(e.target.value)}>{state.nodes.map((n) => <option key={n.id} value={n.id}>{n.label}</option>)}</select>
        </div>
        <div className="inputgroup">
          <label>Line label</label>
          <input value={label} placeholder="e.g. approved" onChange={(e) => setLabel(e.target.value)} />
        </div>
        <div className="inputgroup wide">
          <label>What this line is about</label>
          <input value={about} placeholder="e.g. Supervisor approves and assigns to the operator" onChange={(e) => setAbout(e.target.value)} />
        </div>
        <button className="btn small" type="button" onClick={save}>{state.selectedLinkIndex != null ? "Update connector" : "Save connector"}</button>
        <button className="btn small" type="button" onClick={() => { dispatch({ kind: "selectLink", index: null }); setLabel(""); setAbout(""); }}>Clear editor</button>
      </div>
      <div className="link-list">
        {state.links.map(([lf, lt, ll, la], i) => {
          const fromNode = state.nodes.find((n) => n.id === lf);
          const toNode = state.nodes.find((n) => n.id === lt);
          return (
            <div key={i} className={`link-row${i === state.selectedLinkIndex ? " selected" : ""}`} onClick={() => dispatch({ kind: "selectLink", index: i })}>
              <div>
                <div className="link-title">{fromNode?.label ?? lf} → {toNode?.label ?? lt} <span className="pill teal">{ll || "next"}</span></div>
                <div className="link-copy">{la || "—"}</div>
              </div>
              <div className="link-actions">
                <button className="mini-btn" type="button" onClick={(e) => { e.stopPropagation(); dispatch({ kind: "selectLink", index: i }); }}>Edit</button>
                <button className="mini-btn danger" type="button" onClick={(e) => { e.stopPropagation(); dispatch({ kind: "removeLink", index: i }); }}>Remove</button>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function center(node: BuilderNode): { x: number; y: number } {
  return { x: node.x + NODE_W / 2, y: node.y + 24 };
}

function FlowCanvas({ state, dispatch, lit }: { state: BuilderState; dispatch: Dispatch<Action>; lit: Set<string> }) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const [linking, setLinking] = useState<{ from: string; x: number; y: number } | null>(null);

  const local = (e: { clientX: number; clientY: number }) => {
    const rect = wrapRef.current?.getBoundingClientRect();
    return { x: e.clientX - (rect?.left ?? 0) + (wrapRef.current?.scrollLeft ?? 0), y: e.clientY - (rect?.top ?? 0) + (wrapRef.current?.scrollTop ?? 0) };
  };

  const startNodeDrag = (e: ReactPointerEvent, node: BuilderNode) => {
    if (linking) return;
    dispatch({ kind: "selectNode", id: node.id });
    const sx = e.clientX, sy = e.clientY, ox = node.x, oy = node.y;
    const move = (ev: PointerEvent) => {
      dispatch({ kind: "moveNode", id: node.id, x: Math.max(0, ox + (ev.clientX - sx)), y: Math.max(0, oy + (ev.clientY - sy)) });
    };
    const up = () => { document.removeEventListener("pointermove", move); document.removeEventListener("pointerup", up); };
    document.addEventListener("pointermove", move);
    document.addEventListener("pointerup", up);
  };

  const startConnector = (e: ReactPointerEvent, node: BuilderNode) => {
    e.stopPropagation();
    const sx = e.clientX, sy = e.clientY;
    let dragging = false;
    const move = (ev: PointerEvent) => {
      if (!dragging && Math.hypot(ev.clientX - sx, ev.clientY - sy) < 5) return;
      dragging = true;
      setLinking({ from: node.id, ...local(ev) });
    };
    const up = (ev: PointerEvent) => {
      document.removeEventListener("pointermove", move);
      document.removeEventListener("pointerup", up);
      if (dragging) {
        const target = (document.elementFromPoint(ev.clientX, ev.clientY) as HTMLElement | null)?.closest<HTMLElement>("[data-node-id]");
        const toId = target?.dataset.nodeId;
        if (toId && toId !== node.id) {
          const fromNode = state.nodes.find((n) => n.id === node.id);
          const toNode = state.nodes.find((n) => n.id === toId);
          dispatch({ kind: "saveLink", from: node.id, to: toId, label: suggestLinkLabel(fromNode?.role ?? "", toNode?.role ?? ""), about: "", replaceIndex: null });
        }
      }
      setLinking(null);
    };
    document.addEventListener("pointermove", move);
    document.addEventListener("pointerup", up);
  };

  return (
    <div className={`flowwrap${linking ? " linking" : ""}`} ref={wrapRef}>
      <div className="flowcanvas" style={{ width: CANVAS_W, height: CANVAS_H }}>
        <svg viewBox={`0 0 ${CANVAS_W} ${CANVAS_H}`} width={CANVAS_W} height={CANVAS_H}>
          <defs>
            <marker id="sb-ah" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto"><path d="M0,0 L6,3 L0,6 Z" fill="#33485f" /></marker>
            <marker id="sb-ahl" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto"><path d="M0,0 L6,3 L0,6 Z" fill="#14F1D9" /></marker>
          </defs>
          {state.links.map(([from, to, label], i) => {
            const na = state.nodes.find((n) => n.id === from);
            const nb = state.nodes.find((n) => n.id === to);
            if (!na || !nb) return null;
            const ca = center(na), cb = center(nb);
            const isLit = lit.has(from) && lit.has(to);
            const selected = i === state.selectedLinkIndex;
            const mid = (ca.x + cb.x) / 2;
            const d = `M${ca.x},${ca.y} C${mid},${ca.y} ${mid},${cb.y} ${cb.x},${cb.y}`;
            const stroke = selected ? "#14F1D9" : isLit ? "#14F1D9" : "#33485f";
            return (
              <g key={i} className={`transition-group${selected ? " selected" : ""}`} onClick={() => dispatch({ kind: "selectLink", index: i })}>
                <path className="transition-hit" d={d} />
                <path className="transition-line" d={d} fill="none" stroke={stroke} strokeWidth={selected ? 3 : isLit ? 2 : 1.3} opacity={selected || isLit ? 1 : 0.7} markerEnd={`url(#${isLit || selected ? "sb-ahl" : "sb-ah"})`} />
                {label ? (
                  <text x={mid} y={(ca.y + cb.y) / 2 - 7} textAnchor="middle" fill={selected || isLit ? "#14F1D9" : "#cbd5e1"} stroke="#0d1117" strokeWidth={4} paintOrder="stroke" style={{ fontSize: 10, fontWeight: 700 }}>{label}</text>
                ) : null}
              </g>
            );
          })}
          {linking ? (() => {
            const na = state.nodes.find((n) => n.id === linking.from);
            if (!na) return null;
            const ca = center(na);
            const mid = (ca.x + linking.x) / 2;
            return <path d={`M${ca.x},${ca.y} C${mid},${ca.y} ${mid},${linking.y} ${linking.x},${linking.y}`} fill="none" stroke="#14F1D9" strokeWidth={2} strokeDasharray="7 5" opacity={0.9} />;
          })() : null}
        </svg>
        {state.nodes.map((node, i) => {
          const cls = nodeVisualClass(node, i === 0);
          return (
            <div
              key={node.id}
              data-node-id={node.id}
              className={`node ${cls}${node.id === state.selectedNodeId ? " selected" : ""}${lit.has(node.id) ? " lit" : ""}`}
              style={{ left: node.x, top: node.y }}
              onPointerDown={(e) => startNodeDrag(e, node)}
              onClick={() => dispatch({ kind: "selectNode", id: node.id })}
            >
              <div className="nlabel">{node.label}</div>
              <div className="nrole">{node.role}</div>
              <button className="node-handle" type="button" title="Drag to another box to connect" onPointerDown={(e) => startConnector(e, node)} onClick={(e) => e.stopPropagation()}>+</button>
            </div>
          );
        })}
      </div>
    </div>
  );
}
