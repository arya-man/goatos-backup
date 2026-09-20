// THE TOXIN PROCEDURE IS AUTHORED (maintainer decision 2026-09-20,
// docs/decisions/procurement-sop-driven.md).
//
// `form_dsl.toxin` of the `procurement.toxin_test` SOP: the ordered steps of the aflatoxin strip
// test. Parsing is BYTE-FAITHFUL -- a document loaded and emitted unchanged produces the same
// JSON, so opening the editor and pressing Save never rewrites a step nobody touched
// (pinned by toxin-model.test.mjs).
//
// The problems() below mirror the BACKEND validator rule for rule. They are a preview, never the
// gate: the server runs its own and refuses a document the engine could not run, which is where
// the medical safety actually lives.

export const TOXIN_SCHEMA_VERSION = "goatos.sop-toxin.v1";

export type ToxinStepKind = "video" | "wait" | "photo_reading";

export type ToxinStepRow = {
  /** Stable client id for React keys and selection; never emitted. */
  id: string;
  no: number;
  kind: ToxinStepKind;
  title: string;
  instruction: string;
  waitMinutes: number;
  gateAfterStep: number;
  gateMinutes: number;
};

export type ToxinRows = { steps: ToxinStepRow[] };

type Raw = Record<string, unknown>;

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function num(v: unknown): number {
  return typeof v === "number" && Number.isFinite(v) ? v : 0;
}

/** parseToxin reads form_dsl.toxin; null when the version carries no procedure. */
export function parseToxin(formDsl: unknown): ToxinRows | null {
  if (!formDsl || typeof formDsl !== "object") return null;
  const section = (formDsl as Raw).toxin;
  if (!section || typeof section !== "object") return null;
  const raw = section as Raw;
  if (str(raw.schema_version) !== TOXIN_SCHEMA_VERSION) return null;
  const steps = Array.isArray(raw.steps) ? raw.steps : [];
  return {
    steps: steps.map((s, i) => {
      const r = (s ?? {}) as Raw;
      const kind = str(r.kind);
      return {
        id: `step-${i}`,
        no: num(r.no) || i + 1,
        kind: (kind === "wait" || kind === "photo_reading" ? kind : "video") as ToxinStepKind,
        title: str(r.title),
        instruction: str(r.instruction),
        waitMinutes: num(r.wait_minutes),
        gateAfterStep: num(r.gate_after_step),
        gateMinutes: num(r.gate_minutes),
      };
    }),
  };
}

/** emitToxin writes the document back, renumbering 1..N in the order the editor shows. */
export function emitToxin(rows: ToxinRows): Record<string, unknown> {
  return {
    schema_version: TOXIN_SCHEMA_VERSION,
    steps: rows.steps.map((s, i) => {
      const out: Record<string, unknown> = {
        no: i + 1,
        kind: s.kind,
        title: s.title.trim(),
        instruction: s.instruction.trim(),
      };
      if (s.kind === "wait" && s.waitMinutes > 0) out.wait_minutes = s.waitMinutes;
      if (s.kind !== "wait" && s.gateAfterStep > 0 && s.gateMinutes > 0) {
        out.gate_after_step = s.gateAfterStep;
        out.gate_minutes = s.gateMinutes;
      }
      return out;
    }),
  };
}

export function blankToxinStep(kind: ToxinStepKind, index: number): ToxinStepRow {
  return {
    id: `new-${index}-${Math.random().toString(36).slice(2, 8)}`,
    no: index + 1,
    kind,
    title: "",
    instruction: "",
    waitMinutes: kind === "wait" ? 30 : 0,
    gateAfterStep: 0,
    gateMinutes: 0,
  };
}

/**
 * toxinProblems mirrors the backend's ValidateToxin. Each rule is a way to publish a procedure
 * that would strand a round mid-test, which is why they are stated here too: an author should see
 * them while editing, not when the server refuses the publish.
 */
export function toxinProblems(rows: ToxinRows, t: (key: string) => string): string[] {
  const problems: string[] = [];
  if (rows.steps.length === 0) {
    problems.push(t("tsop.problem.no_steps"));
    return problems;
  }
  const readings = rows.steps.filter((s) => s.kind === "photo_reading");
  if (readings.length !== 1) problems.push(t("tsop.problem.one_reading"));
  else if (rows.steps[rows.steps.length - 1].kind !== "photo_reading") problems.push(t("tsop.problem.reading_last"));
  const working = rows.steps.filter((s) => s.kind !== "wait");
  if (working.length < 2) problems.push(t("tsop.problem.too_short"));
  rows.steps.forEach((s, i) => {
    const at = `${i + 1}. `;
    if (!s.title.trim()) problems.push(at + t("tsop.problem.title"));
    if (!s.instruction.trim()) problems.push(at + t("tsop.problem.instruction"));
    if (s.kind === "wait" && s.waitMinutes <= 0) problems.push(at + t("tsop.problem.wait_minutes"));
    if (s.kind !== "wait" && s.gateAfterStep > 0) {
      if (s.gateAfterStep >= i + 1) problems.push(at + t("tsop.problem.gate_backwards"));
      else if (rows.steps[s.gateAfterStep - 1]?.kind === "wait") problems.push(at + t("tsop.problem.gate_working"));
      if (s.gateMinutes <= 0) problems.push(at + t("tsop.problem.gate_minutes"));
    }
  });
  return problems;
}

/** toxinStepCount is what the library card shows: the steps that record a completion. */
export function toxinWorkingStepCount(rows: ToxinRows): number {
  return rows.steps.filter((s) => s.kind !== "wait").length;
}
