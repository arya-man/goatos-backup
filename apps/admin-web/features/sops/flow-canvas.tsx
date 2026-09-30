"use client";

import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import ButtonBase from "@mui/material/ButtonBase";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Typography from "@mui/material/Typography";
import type { SxProps, Theme } from "@mui/material/styles";
import { varAlpha } from "minimal-shared/utils";
import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from "react";

import { Iconify, type IconifyName } from "@/components/minimal/iconify";
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

/** Top accent per node kind (template palette; the chart reads the same in light and dark). */
const KIND_ACCENT: Partial<Record<CanvasNode["kind"], string>> = {
  start: "primary.dark",
  question: "info.main",
  decision: "warning.main",
  group: "secondary.main",
  finish: "text.disabled",
};

function nodeSx(node: CanvasNode, selected: boolean, clickable: boolean): SxProps<Theme> {
  const kind = node.kind;
  const accent = KIND_ACCENT[kind];
  return (theme) => ({
    left: node.x,
    top: node.y,
    width: node.w,
    height: node.h,
    position: "absolute",
    zIndex: 1,
    display: "flex",
    flexDirection: kind === "decision" ? "row" : "column",
    alignItems: kind === "decision" ? "center" : "stretch",
    justifyContent: kind === "decision" ? "space-between" : "center",
    gap: kind === "decision" ? 1 : 0.25,
    px: 1.5,
    py: 1,
    boxSizing: "border-box",
    textAlign: "left",
    color: "text.primary",
    bgcolor: kind === "decision" ? "background.neutral" : "background.paper",
    border: `1px ${kind === "fixed" ? "dashed" : "solid"} ${theme.vars.palette.divider}`,
    borderRadius: "var(--r-lg)",
    ...(accent ? { borderTopWidth: 3, borderTopStyle: "solid", borderTopColor: accent } : {}),
    ...(kind === "fixed" ? { opacity: 0.85 } : {}),
    ...(clickable
      ? {
          cursor: "pointer",
          "&:hover": { borderColor: "primary.dark" },
          "&:focus-visible": { outline: `2px solid ${theme.vars.palette.primary.dark}`, outlineOffset: 2 },
        }
      : {}),
    ...(selected ? { borderColor: "primary.dark", boxShadow: `0 0 0 2px ${varAlpha(theme.vars.palette.primary.darkChannel, 0.3)}` } : {}),
  });
}

/** The small uppercase line naming a node's kind ("STEP", "DECISION"), optionally with a glyph. */
export function NodeKind({ icon, children }: { icon?: IconifyName; children: ReactNode }) {
  return (
    <Box component="span" sx={{ display: "flex", alignItems: "center", gap: 0.5, typography: "overline", lineHeight: 1.4, color: "text.secondary" }}>
      {icon ? <Iconify icon={icon} width={12} /> : null}
      {children}
    </Box>
  );
}

/** The kind of the node being rendered, so its title clamps like the chart expects. */
const NodeKindContext = createContext<CanvasNode["kind"]>("step");
const ONE_LINE_KINDS: CanvasNode["kind"][] = ["start", "finish", "fixed", "group"];

/** The node's title: two lines (one on start / finish / fixed / group boxes), then an ellipsis. */
export function NodeTitle({ children, lines: linesIn, title }: { children: ReactNode; lines?: number; title?: string }) {
  const kind = useContext(NodeKindContext);
  const lines = linesIn ?? (ONE_LINE_KINDS.includes(kind) ? 1 : 2);
  return (
    <Typography
      component="b"
      variant="subtitle2"
      title={title}
      sx={{ lineHeight: 1.2, flexShrink: 0, overflow: "hidden", textOverflow: "ellipsis", display: "-webkit-box", WebkitLineClamp: lines, WebkitBoxOrient: "vertical" }}
    >
      {children}
    </Typography>
  );
}

/** A node's secondary line (answer kind, proof, hint); clamped when `lines` is set. */
export function NodeNote({ children, lines }: { children: ReactNode; lines?: number }) {
  return (
    <Typography
      component="span"
      variant="caption"
      sx={{ color: "text.secondary", ...(lines ? { overflow: "hidden", flexShrink: 0, display: "-webkit-box", WebkitLineClamp: lines, WebkitBoxOrient: "vertical" } : {}) }}
    >
      {children}
    </Typography>
  );
}

/** The owner chip on a step ("For: Supervisor"). */
export function NodeOwner({ children, testId }: { children: ReactNode; testId?: string }) {
  return (
    <Box
      component="span"
      data-testid={testId}
      sx={{
        alignSelf: "flex-start",
        maxWidth: 1,
        px: 1,
        typography: "caption",
        color: "text.secondary",
        bgcolor: "background.neutral",
        border: 1,
        borderColor: "divider",
        borderRadius: "var(--r-pill)",
        whiteSpace: "nowrap",
        overflow: "hidden",
        textOverflow: "ellipsis",
      }}
    >
      {children}
    </Box>
  );
}

/** A row of in-node actions (Add branch). */
export function NodeActions({ children }: { children: ReactNode }) {
  return (
    <Box component="span" sx={{ display: "flex", gap: 0.5, flexWrap: "wrap" }}>
      {children}
    </Box>
  );
}

/** A compact template text Button inside a node ("+ Add branch"). */
export function NodeButton({ onClick, testId, children }: { onClick: (event: React.MouseEvent<HTMLButtonElement>) => void; testId?: string; children: ReactNode }) {
  return (
    <Button
      size="small"
      color="inherit"
      onClick={onClick}
      data-testid={testId}
      startIcon={<Iconify icon="mingcute:add-line" width={14} />}
      sx={{ flex: "none", whiteSpace: "nowrap", typography: "caption", fontWeight: "fontWeightSemiBold", px: 0.75 }}
    >
      {children}
    </Button>
  );
}

/**
 * The Flow studio layout (template two-column: chart, then the properties card on the right; one
 * column below lg). The properties card is exactly as tall as the chart column and scrolls inside
 * itself, so a tall step's fields never stretch the row.
 */
export function FlowStudio({ pc, canvas, children }: { pc: AdminUiPageContract; canvas: ReactNode; children: ReactNode }) {
  return (
    <Box data-testid="flow-view" sx={{ display: "grid", gridTemplateColumns: { xs: "minmax(0, 1fr)", lg: "minmax(0, 1fr) 380px" }, gap: 1.75, alignItems: "stretch", mt: 1.25 }}>
      {canvas}
      <Card component="aside" data-testid="flow-props" sx={{ boxSizing: "border-box", overflow: "auto", height: { lg: 0 }, minHeight: { lg: "100%" }, maxHeight: { xs: "60vh", lg: "none" } }}>
        <CardHeader title={copy(pc, "studio.flow.properties")} sx={{ mb: 1.5 }} />
        <Box sx={{ px: 1.75, pb: 1.75 }}>{children}</Box>
      </Card>
    </Box>
  );
}

/** The properties card's "nothing selected" line. */
export function FlowNoneSelected({ pc }: { pc: AdminUiPageContract }) {
  return (
    <Box role="status" sx={{ display: "flex", alignItems: "center", gap: 1, p: 0.5, color: "text.secondary", typography: "body2" }}>
      <Iconify icon="solar:info-circle-bold" width={18} />
      {copy(pc, "studio.flow.none_selected")}
    </Box>
  );
}

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
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1, minWidth: 0 }}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 0.75, flexWrap: "wrap" }}>
        <IconButton label={copy(pc, "studio.flow.zoom_out")} icon="mingcute:minimize-line" onClick={() => setZoom((z) => Math.max(0.35, Math.round((z - 0.1) * 100) / 100))} />
        <Typography variant="body2" sx={{ color: "text.secondary" }} data-testid="flow-zoom">
          {Math.round(zoom * 100)}%
        </Typography>
        <IconButton label={copy(pc, "studio.flow.zoom_in")} icon="mingcute:add-line" onClick={() => setZoom((z) => Math.min(1.5, Math.round((z + 0.1) * 100) / 100))} />
        <Button size="small" color="inherit" onClick={fit} startIcon={<Iconify icon="carbon:fit-to-screen" width={16} />}>
          {copy(pc, "studio.flow.fit")}
        </Button>
        <Typography variant="body2" sx={{ color: "text.secondary", ml: "auto" }}>
          {hint ?? copy(pc, "studio.flow.select_hint")}
        </Typography>
      </Box>
      <Box
        ref={canvasRef}
        sx={{
          position: "relative",
          overflow: "auto",
          border: 1,
          borderColor: "divider",
          borderRadius: "var(--r-lg)",
          background: "radial-gradient(circle, var(--palette-divider) 1px, transparent 1px) 0 0 / 22px 22px, var(--palette-background-default)",
          minHeight: "50vh",
          maxHeight: "72vh",
          p: 1.5,
        }}
      >
        {/* The sizer takes the SCALED size and is centred, so a fitted flow sits in the middle of
            the canvas instead of leaving the unscaled width as empty space beside it. */}
        <Box sx={{ width: layout.width * zoom, height: layout.height * zoom, mx: "auto", position: "relative" }}>
          <Box sx={{ position: "relative", transformOrigin: "0 0", width: layout.width, height: layout.height, transform: `scale(${zoom})` }}>
            <Box
              component="svg"
              width={layout.width}
              height={layout.height}
              aria-hidden="true"
              sx={{ position: "absolute", left: 0, top: 0, overflow: "visible", pointerEvents: "none", color: "primary.dark" }}
            >
              {layout.edges.map((e) => {
                const a = anchor(e.from).bottom;
                const b = anchor(e.to).top;
                const my = (a.y + b.y) / 2;
                return (
                  <path key={e.id} d={`M ${a.x} ${a.y} C ${a.x} ${my}, ${b.x} ${my}, ${b.x} ${b.y}`} fill="none" stroke="currentColor" strokeWidth={2} opacity={0.75} markerEnd="url(#studio-arrow)" />
                );
              })}
              <defs>
                <marker id="studio-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse">
                  <path d="M 0 0 L 10 5 L 0 10 z" fill="currentColor" />
                </marker>
              </defs>
            </Box>
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
                    <Box
                      component="span"
                      title={e.label}
                      sx={{
                        position: "absolute",
                        left: label.x,
                        top: label.y,
                        transform: "translate(-50%, -50%)",
                        zIndex: 2,
                        typography: "caption",
                        color: "text.secondary",
                        bgcolor: "background.paper",
                        border: 1,
                        borderColor: "divider",
                        borderRadius: "var(--r-pill)",
                        px: 1,
                        py: 0.25,
                        whiteSpace: "nowrap",
                        maxWidth: 240,
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                      }}
                    >
                      {e.label}
                    </Box>
                  ) : null}
                  {e.insert !== undefined ? (
                    <ButtonBase
                      title={copy(pc, "studio.flow.insert")}
                      aria-label={copy(pc, "studio.flow.insert")}
                      onClick={() => onInsert(e.insert as I, e)}
                      data-testid={`flow-insert-${tidOf(e.from)}-${tidOf(e.to)}`}
                      sx={plusSx(plus.x, plus.y, zoom)}
                    >
                      <Box component="span" aria-hidden="true" sx={plusDotSx}>
                        +
                      </Box>
                    </ButtonBase>
                  ) : null}
                </div>
              );
            })}
            {layout.nodes.map((n) => {
              const selected = selectedId === n.id;
                if (n.selectable) {
                // A div with the button role rather than a <button>: a question node carries its own
                // "Add branch" button (edge-case audit 2026-09-18), and a button inside a button is
                // invalid HTML. Enter/Space select it exactly as a button would.
                return (
                  <Box
                    key={n.id}
                    role="button"
                    tabIndex={0}
                    sx={nodeSx(n, selected, true)}
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
                    <NodeKindContext.Provider value={n.kind}>{renderNode(n)}</NodeKindContext.Provider>
                  </Box>
                );
              }
              return (
                <Box key={n.id} sx={nodeSx(n, selected, false)} data-testid={`flow-node-${n.tid}`}>
                  <NodeKindContext.Provider value={n.kind}>{renderNode(n)}</NodeKindContext.Provider>
                </Box>
              );
            })}
          </Box>
        </Box>
      </Box>
    </Box>
  );
}

/** The + on a line: a 22px ring that stays the same size at any zoom; a 44px hit box on a phone. */
function plusSx(x: number, y: number, zoom: number): SxProps<Theme> {
  return {
    position: "absolute",
    left: x,
    top: y,
    zIndex: 2,
    transform: `translate(-50%, -50%) scale(${1 / zoom})`,
    width: { xs: 44, lg: 22 },
    height: { xs: 44, lg: 22 },
    borderRadius: "var(--r-round)",
    color: "primary.dark",
    "&:hover > span": { bgcolor: "primary.dark", color: "primary.contrastText" },
  };
}
const plusDotSx: SxProps<Theme> = {
  display: "grid",
  placeItems: "center",
  width: 22,
  aspectRatio: "1",
  borderRadius: "var(--r-round)",
  border: 1,
  borderColor: "currentColor",
  bgcolor: "background.paper",
  typography: "subtitle2",
  lineHeight: 1,
};

function IconButton({ label, icon, onClick }: { label: string; icon: IconifyName; onClick: () => void }) {
  return (
    <Button size="small" color="inherit" onClick={onClick} aria-label={label} sx={{ minWidth: 0, px: 1 }}>
      <Iconify icon={icon} width={16} />
    </Button>
  );
}
