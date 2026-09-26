"use client";

import { Minus, Plus, Scan } from "lucide-react";
import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

/** A positioned box on the chart. `kind` picks its styling; `tid` is its stable test id. */
export type CanvasNode<T = unknown> = {
  id: string;
  tid: string;
  kind: "start" | "step" | "question" | "decision" | "finish" | "fixed" | "group";
  x: number;
  y: number;
  w: number;
  h: number;
  data?: T;
  selectable?: boolean;
};

/** A line between two boxes; `insert` is what a + on it does (undefined = no insert point). */
export type CanvasEdge<I = unknown> = {
  id: string;
  from: string;
  to: string;
  label: string;
  insert?: I;
};

export type CanvasLayout<T = unknown, I = unknown> = { nodes: CanvasNode<T>[]; edges: CanvasEdge<I>[]; width: number; height: number };

/**
 * The shared chart surface of the SOP studio's Flow views (2026-09-18): boxes, curved lines
 * with an insert + and a label at the fork, zoom / fit. Layout comes in positioned; what a
 * box shows and what an insert does belong to the caller.
 */
export function FlowCanvas<T, I>({
  pc,
  layout,
  selectedId,
  onSelect,
  onInsert,
  renderNode,
  hint,
}: {
  pc: AdminUiPageContract;
  layout: CanvasLayout<T, I>;
  selectedId: string;
  onSelect: (node: CanvasNode<T>) => void;
  onInsert: (insert: I, edge: CanvasEdge<I>) => void;
  renderNode: (node: CanvasNode<T>) => ReactNode;
  hint?: string;
}) {
  const [zoom, setZoom] = useState(1);
  const canvasRef = useRef<HTMLDivElement>(null);
  const fit = () => {
    const el = canvasRef.current;
    if (!el) return;
    const scale = Math.min(1, (el.clientWidth - 24) / layout.width);
    // On a phone a fit below ~70% made the nodes unreadable (FJ3 P1-17): keep 70% and let the
    // canvas pan sideways instead.
    const floor = el.clientWidth < 600 ? 0.7 : 0.35;
    setZoom(Math.max(floor, Math.round(scale * 100) / 100));
  };
  useEffect(() => {
    fit();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [layout.width]);
  const byId = new Map(layout.nodes.map((n) => [n.id, n]));
  const anchor = (id: string) => {
    const n = byId.get(id)!;
    return { top: { x: n.x + n.w / 2, y: n.y }, bottom: { x: n.x + n.w / 2, y: n.y + n.h } };
  };
  const tidOf = (id: string) => byId.get(id)?.tid ?? id;
  return (
    <div className="studio-flow-canvas-wrap">
      <div className="studio-flow-toolbar">
        <button type="button" className="btn sm ghost" onClick={() => setZoom((z) => Math.max(0.35, Math.round((z - 0.1) * 100) / 100))} aria-label={copy(pc, "studio.flow.zoom_out")}>
          <Minus size={14} />
        </button>
        <span className="muted small" data-testid="flow-zoom">
          {Math.round(zoom * 100)}%
        </span>
        <button type="button" className="btn sm ghost" onClick={() => setZoom((z) => Math.min(1.5, Math.round((z + 0.1) * 100) / 100))} aria-label={copy(pc, "studio.flow.zoom_in")}>
          <Plus size={14} />
        </button>
        <button type="button" className="btn sm ghost" onClick={fit}>
          <Scan size={14} /> {copy(pc, "studio.flow.fit")}
        </button>
        <span className="muted small studio-flow-hint">{hint ?? copy(pc, "studio.flow.select_hint")}</span>
      </div>
      <div className="studio-flow-canvas" ref={canvasRef}>
        {/* The sizer takes the SCALED size and is centred, so a fitted flow sits in the middle of
            the canvas instead of leaving the unscaled width as empty space beside it. */}
        <div style={{ width: layout.width * zoom, height: layout.height * zoom, margin: "0 auto", position: "relative" }}>
        <div
          className="studio-flow-scale"
          style={{
            width: layout.width,
            height: layout.height,
            transform: `scale(${zoom})`,
            "--studio-flow-zoom": zoom,
          } as CSSProperties}
        >
          <svg className="studio-flow-edges" width={layout.width} height={layout.height} aria-hidden="true">
            {layout.edges.map((e) => {
              const a = anchor(e.from).bottom;
              const b = anchor(e.to).top;
              const my = (a.y + b.y) / 2;
              return <path key={e.id} d={`M ${a.x} ${a.y} C ${a.x} ${my}, ${b.x} ${my}, ${b.x} ${b.y}`} className="studio-flow-edge" markerEnd="url(#studio-arrow)" />;
            })}
            <defs>
              <marker id="studio-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse">
                <path d="M 0 0 L 10 5 L 0 10 z" className="studio-flow-arrow" />
              </marker>
            </defs>
          </svg>
          {layout.edges.map((e) => {
            const a = anchor(e.from).bottom;
            const b = anchor(e.to).top;
            const at = (t: number) => {
              const my = (a.y + b.y) / 2;
              const u = 1 - t;
              return {
                x: u * u * u * a.x + 3 * u * u * t * a.x + 3 * u * t * t * b.x + t * t * t * b.x,
                y: u * u * u * a.y + 3 * u * u * t * my + 3 * u * t * t * my + t * t * t * b.y,
              };
            };
            const plus = at(e.label ? 0.62 : 0.5);
            const label = at(0.42);
            return (
              <div key={e.id + ":ctl"}>
                {e.label ? (
                  <span className="studio-flow-edge-label" style={{ left: label.x, top: label.y }} title={e.label}>
                    {e.label}
                  </span>
                ) : null}
                {e.insert !== undefined ? (
                  <button type="button" className="studio-flow-plus" style={{ left: plus.x, top: plus.y }} title={copy(pc, "studio.flow.insert")} aria-label={copy(pc, "studio.flow.insert")} onClick={() => onInsert(e.insert as I, e)} data-testid={`flow-insert-${tidOf(e.from)}-${tidOf(e.to)}`}>
                    +
                  </button>
                ) : null}
              </div>
            );
          })}
          {layout.nodes.map((n) => {
            const cls = `studio-node studio-node-${n.kind}${selectedId === n.id ? " on" : ""}`;
            const style = { left: n.x, top: n.y, width: n.w, height: n.h };
            if (n.selectable) {
              // A div with the button role rather than a <button>: a question node carries its own
              // "Add branch" button (edge-case audit 2026-09-18), and a button inside a button is
              // invalid HTML. Enter/Space select it exactly as a button would.
              return (
                <div
                  key={n.id}
                  role="button"
                  tabIndex={0}
                  className={cls}
                  style={style}
                  onClick={() => onSelect(n)}
                  onKeyDown={(e) => {
                    if (e.target !== e.currentTarget) return;
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      onSelect(n);
                    }
                  }}
                  data-testid={`flow-node-${n.tid}`}
                >
                  {renderNode(n)}
                </div>
              );
            }
            return (
              <div key={n.id} className={cls} style={style} data-testid={`flow-node-${n.tid}`}>
                {renderNode(n)}
              </div>
            );
          })}
        </div>
        </div>
      </div>
    </div>
  );
}
