"use client";
import Box from "@mui/material/Box";

import { Tag } from "@/components/ui-primitives";

// WEIGHING SOP (maintainer decision 2026-09-15, docs/decisions/weighing-sop.md).
//
// The weighing rules editor: what the planner may choose, whether the evening-before feed &
// water removal applies (always / per task / never), what the removal card says and asks, and
// what the operator captures and answers PER ANIMAL and, separately, for the WHOLE PEN (the weigh
// captures are authored, maintainer decision 2026-09-16: two sections, never one shared list).
// Every word on this screen is the backend contract's; the
// backend validates the document on save and refuses one the planner could not run, naming the
// field. The scan-and-submit rules (free-flow capture, per-animal video, verification, close)
// are maintainer locks and are shown, not edited.
import { useMemo, useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { AlertTriangle, Check, ChevronDown, ChevronUp, Lock, Plus, X } from "lucide-react";
import IconButton from "@mui/material/IconButton";
import MuiTextField from "@mui/material/TextField";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import { EditorHeader, InlineSelect, StickyActions, StudioViewToggle, inspectionEditorSx } from "./editor-chrome";
import {
  CAPTURE_DEFAULTS_COPY_KEY,
  LUMP_SUM_VIDEO_CEILING,
  WEIGHING_MODES,
  blankCountedSlot,
  blankProofSlot,
  blankQuestion,
  emitWeighing,
  legacyVideoWindow,
  parseCaptureDefaults,
  followQuestionKey,
  keyForTitle,
  slugKey,
  weighingProblems,
  withCaptureDefaults,
  type CountedProofRow,
  type RemovalMode,
  type RemovalProofKind,
  type RemovalProofRow,
  type WeighingMode,
  type WeighingQuestionKind,
  type WeighingQuestionRow,
  type WeighingRows,
} from "./weighing-model";
import { publishedHref } from "./published-href";
import { WeighingFlow, isQuestionList, type WeighingInsert, type WeighingRef } from "./weighing-flow";
import { publishWeighingVersion, saveWeighingVersion, type WeighingSaveResult } from "./sop-actions";
import Checkbox from "@mui/material/Checkbox";
import Radio from "@mui/material/Radio";
import FormControlLabel from "@mui/material/FormControlLabel";
import Alert from "@mui/material/Alert";

type Props = {
  pageContract: AdminUiPageContract;
  basePath: string;
  sopId: string;
  sopName: string;
  sopCode: string;
  versionLabel: string;
  initial: WeighingRows;
  /** The view the page opened on (`?view=flow`), read on the server so SSR and client agree. */
  initialView?: "list" | "flow";
};

export function WeighingEditor({ pageContract: pc, basePath, sopId, sopName, versionLabel, initial, initialView = "list" }: Props) {
  const router = useRouter();
  // The editor is handed rows parsed before the page contract was in hand; the contract's seeded
  // slot document (`wsop.capture.defaults`) fills the sections the document left implicit.
  const [rows, setRows] = useState<WeighingRows>(() => withCaptureDefaults(initial, parseCaptureDefaults(copy(pc, CAPTURE_DEFAULTS_COPY_KEY, ""))));
  // Keys the loaded version already carries never move (phones stamp them, answers are stored under
  // them); a new capture / question key follows its whole title (keyForTitle).
  const [savedKeys] = useState<Set<string>>(
    () =>
      new Set(
        [
          ...initial.removalProofs, ...initial.individualProofs, ...initial.lumpSumProofs,
          ...initial.removalQuestions, ...initial.individualQuestions, ...initial.lumpSumQuestions,
        ].map((x) => x.key).filter(Boolean),
      ),
  );
  const [result, setResult] = useState<WeighingSaveResult | null>(null);
  const [pending, startTransition] = useTransition();
  // LIST (default) / FLOW (the chart of the session: plan, removal card, Per animal | Whole pen).
  const [view, setView] = useState<"list" | "flow">(initialView);
  const [selectedRef, setSelectedRef] = useState<WeighingRef | null>(null);
  function switchView(next: "list" | "flow") {
    setView(next);
    if (typeof window !== "undefined") {
      const url = new URL(window.location.href);
      if (next === "flow") url.searchParams.set("view", "flow");
      else url.searchParams.delete("view");
      window.history.replaceState(window.history.state, "", url.toString());
    }
  }
  // Insert at an index (the chart's + on a line); the List view's add buttons append.
  function insertAt(insert: WeighingInsert) {
    const row = isQuestionList(insert.list) ? blankQuestion() : insert.list === "lumpSumProofs" ? blankCountedSlot() : blankProofSlot();
    setRows((r) => {
      const list = [...(r[insert.list] as { id: string }[])];
      list.splice(Math.min(Math.max(insert.index, 0), list.length), 0, row);
      return { ...r, [insert.list]: list };
    });
    setSelectedRef({ list: insert.list, id: row.id });
  }
  // The chart's properties panel renders the same card the List view shows for that row.
  function cardFor(ref: WeighingRef) {
    if (isQuestionList(ref.list)) {
      const list = ref.list as QuestionList;
      const qs = rows[list];
      const qi = qs.findIndex((q) => q.id === ref.id);
      if (qi < 0) return null;
      const q = qs[qi];
      return (
        <div className="qlist">
          <QuestionCard
            key={q.id}
            pc={pc}
            index={qi}
            count={qs.length}
            q={q}
            kinds={kinds}
            earlier={qs.slice(0, qi)}
            takenKeys={new Set(qs.map((x) => x.key))}
            savedKeys={savedKeys}
            onChange={(patch) => updateQuestion(list, q.id, patch)}
            onOptionRenamed={(from, to) => renameOptionRefs(list, q.key, from, to)}
            onMove={(dir) => moveQuestion(list, q.id, dir)}
            onRemove={() => {
              removeQuestion(list, q.id);
              setSelectedRef(null);
            }}
          />
        </div>
      );
    }
    if (ref.list === "lumpSumProofs") {
      const i = rows.lumpSumProofs.findIndex((p) => p.id === ref.id);
      return i < 0 ? null : <div className="qlist">{countedCard(rows.lumpSumProofs[i], i)}</div>;
    }
    const list = ref.list as SlotList;
    const i = rows[list].findIndex((p) => p.id === ref.id);
    const labels = list === "removalProofs" ? { title: "wsop.removal.proof.title", hint: "wsop.removal.proof.hint", remove: "wsop.removal.proof.remove" } : { title: "wsop.capture.proof.title", hint: "wsop.capture.proof.hint", remove: "wsop.capture.proof.remove" };
    return i < 0 ? null : <div className="qlist">{slotCard(list, rows[list][i], i, labels)}</div>;
  }
  const kinds = optionGroup(pc, "wsop_question_kinds");
  const removalModes = optionGroup(pc, "wsop_removal_modes");
  const proofKinds = optionGroup(pc, "wsop_proof_kinds");
  const proofKindLabels = useMemo(() => Object.fromEntries(proofKinds.map((k) => [k.key, k.label])), [proofKinds]);
  const problems = useMemo(() => weighingProblems(rows), [rows]);
  const lumpWindow = useMemo(() => legacyVideoWindow(rows.lumpSumProofs), [rows.lumpSumProofs]);

  function toggleMode(mode: WeighingMode, on: boolean) {
    setRows((r) => ({ ...r, modes: on ? WEIGHING_MODES.filter((m) => m === mode || r.modes.includes(m)) : r.modes.filter((m) => m !== mode) }));
  }

  // Three question lists (removal card, per animal, whole pen) and three slot lists share the
  // same row mechanics; each list is addressed by its field so an edit can never cross lists.
  type QuestionList = "removalQuestions" | "individualQuestions" | "lumpSumQuestions";
  type SlotList = "removalProofs" | "individualProofs";
  function move<T extends { id: string }>(list: T[], id: string, dir: -1 | 1): T[] {
    const i = list.findIndex((x) => x.id === id);
    const j = i + dir;
    if (i < 0 || j < 0 || j >= list.length) return list;
    const next = [...list];
    [next[i], next[j]] = [next[j], next[i]];
    return next;
  }
  function updateQuestion(list: QuestionList, qid: string, patch: Partial<WeighingQuestionRow>) {
    setRows((r) => ({ ...r, [list]: followQuestionKey(r[list], qid, patch) }));
  }
  function moveQuestion(list: QuestionList, qid: string, dir: -1 | 1) {
    setRows((r) => ({ ...r, [list]: move(r[list], qid, dir) }));
  }
  function removeQuestion(list: QuestionList, qid: string) {
    setRows((r) => ({ ...r, [list]: r[list].filter((q) => q.id !== qid) }));
  }
  function addQuestion(list: QuestionList) {
    setRows((r) => ({ ...r, [list]: [...r[list], blankQuestion()] }));
  }
  // A renamed choice value: every question in the same list conditioned on (questionKey, oldValue) follows.
  function renameOptionRefs(list: QuestionList, questionKey: string, from: string, to: string) {
    setRows((r) => ({
      ...r,
      [list]: r[list].map((q) => (q.onlyIfQuestion === questionKey && q.onlyIfValue === from ? { ...q, onlyIfValue: to } : q)),
    }));
  }
  function updateSlot(list: SlotList, id: string, patch: Partial<RemovalProofRow>) {
    setRows((r) => ({ ...r, [list]: r[list].map((p) => (p.id === id ? { ...p, ...patch } : p)) }));
  }
  function moveSlot(list: SlotList, id: string, dir: -1 | 1) {
    setRows((r) => ({ ...r, [list]: move(r[list], id, dir) }));
  }
  function removeSlot(list: SlotList, id: string) {
    setRows((r) => ({ ...r, [list]: r[list].filter((p) => p.id !== id) }));
  }
  function addSlot(list: SlotList) {
    setRows((r) => ({ ...r, [list]: [...r[list], blankProofSlot()] }));
  }
  function updateCounted(id: string, patch: Partial<CountedProofRow>) {
    setRows((r) => ({ ...r, lumpSumProofs: r.lumpSumProofs.map((p) => (p.id === id ? { ...p, ...patch } : p)) }));
  }
  function moveCounted(id: string, dir: -1 | 1) {
    setRows((r) => ({ ...r, lumpSumProofs: move(r.lumpSumProofs, id, dir) }));
  }
  function removeCounted(id: string) {
    setRows((r) => ({ ...r, lumpSumProofs: r.lumpSumProofs.filter((p) => p.id !== id) }));
  }
  function addCounted() {
    setRows((r) => ({ ...r, lumpSumProofs: [...r.lumpSumProofs, blankCountedSlot()] }));
  }

  function questionList(list: QuestionList, emptyKey: string) {
    const qs = rows[list];
    const taken = new Set(qs.map((q) => q.key));
    return (
      <div className="qlist">
        {qs.length === 0 ? <p className="muted">{copy(pc, emptyKey)}</p> : null}
        {qs.map((q, qi) => (
          <QuestionCard
            key={q.id}
            pc={pc}
            index={qi}
            count={qs.length}
            q={q}
            kinds={kinds}
            earlier={qs.slice(0, qi)}
            takenKeys={taken}
            savedKeys={savedKeys}
            onChange={(patch) => updateQuestion(list, q.id, patch)}
            onOptionRenamed={(from, to) => renameOptionRefs(list, q.key, from, to)}
            onMove={(dir) => moveQuestion(list, q.id, dir)}
            onRemove={() => removeQuestion(list, q.id)}
          />
        ))}
        <Button color="primary" variant="text" size="small" startIcon={<Plus size={14} />} onClick={() => addQuestion(list)}>
            {copy(pc, "inspection.question.add")}
          </Button>
      </div>
    );
  }

  // One capture slot card (removal card and per-animal share it): kind, title, instruction,
  // key while blank, compulsory.
  function slotCard(list: SlotList, p: RemovalProofRow, i: number, labels: { title: string; hint: string; remove: string }) {
    const slots = rows[list];
    return (
      <div className="qcard" key={p.id}>
        <div className="qhead">
          <span className="qnum">{i + 1}</span>
          <span className="qtype">
            <InlineSelect
              label={copy(pc, "wsop.removal.proofs")}
              value={p.kind}
              minWidth={168}
              options={proofKinds.map((k) => ({ value: k.key, label: k.label }))}
              onChange={(next) => updateSlot(list, p.id, { kind: next as RemovalProofKind })}
            />
          </span>
          {p.key ? <code className="muted small">{p.key}</code> : null}
          <span className="sp" style={{ flex: 1 }} />
          <IconButton type="button" size="small" className="ia" aria-label={copy(pc, "inspection.question.move_up")} disabled={i === 0} onClick={() => moveSlot(list, p.id, -1)}>
            <ChevronUp className="ic" />
          </IconButton>
          <IconButton type="button" size="small" className="ia" aria-label={copy(pc, "inspection.question.move_down")} disabled={i === slots.length - 1} onClick={() => moveSlot(list, p.id, 1)}>
            <ChevronDown className="ic" />
          </IconButton>
          <IconButton type="button" size="small" className="ia del" aria-label={copy(pc, labels.remove)} onClick={() => removeSlot(list, p.id)}>
            <X className="ic" />
          </IconButton>
        </div>
        <div className="qbody">
          <label className="numfield">
            <span className="numlbl">{copy(pc, labels.title)}</span>
            <MuiTextField
              fullWidth
              size="small"
              className="qtext"
              value={p.title}
              onChange={(e) => {
                const title = e.target.value;
                updateSlot(list, p.id, { title, key: keyForTitle(title, p.key, savedKeys, new Set(slots.map((x) => x.key)), "capture") });
              }}
            />
          </label>
          <label className="numfield">
            <span className="numlbl">{copy(pc, labels.hint)}</span>
            <MuiTextField fullWidth size="small" value={p.hint} onChange={(e) => updateSlot(list, p.id, { hint: e.target.value })} />
          </label>
          {!p.key ? (
            <label className="numfield">
              <span className="numlbl">{copy(pc, "inspection.question.key")}</span>
              <MuiTextField fullWidth size="small" value={p.key} onChange={(e) => updateSlot(list, p.id, { key: e.target.value })} />
            </label>
          ) : null}
          <div className="qfoot">
            <FormControlLabel className="chkline" control={<Checkbox checked={p.required} onChange={(e) => updateSlot(list, p.id, { required: e.target.checked })} sx={{ p: { xs: 1.5, sm: 1 } }} />} label={<>{copy(pc, "inspection.question.required")}</>} />
          </div>
        </div>
      </div>
    );
  }

  // A whole-pen slot card: the same identity, then a COUNT -- how many the operator must record
  // at least and may record at most -- instead of a compulsory tick.
  function countedCard(p: CountedProofRow, i: number) {
    return (
      <div className="qcard" key={p.id}>
        <div className="qhead">
          <span className="qnum">{i + 1}</span>
          <span className="qtype">
            <InlineSelect
              label={copy(pc, "wsop.capture.proof.title")}
              value={p.kind}
              minWidth={168}
              options={proofKinds.map((k) => ({ value: k.key, label: k.label }))}
              onChange={(next) => updateCounted(p.id, { kind: next as RemovalProofKind })}
            />
          </span>
          {p.key ? <code className="muted small">{p.key}</code> : null}
          <span className="sp" style={{ flex: 1 }} />
          <IconButton type="button" size="small" className="ia" aria-label={copy(pc, "inspection.question.move_up")} disabled={i === 0} onClick={() => moveCounted(p.id, -1)}>
            <ChevronUp className="ic" />
          </IconButton>
          <IconButton type="button" size="small" className="ia" aria-label={copy(pc, "inspection.question.move_down")} disabled={i === rows.lumpSumProofs.length - 1} onClick={() => moveCounted(p.id, 1)}>
            <ChevronDown className="ic" />
          </IconButton>
          <IconButton type="button" size="small" className="ia del" aria-label={copy(pc, "wsop.capture.proof.remove")} onClick={() => removeCounted(p.id)}>
            <X className="ic" />
          </IconButton>
        </div>
        <div className="qbody">
          <label className="numfield">
            <span className="numlbl">{copy(pc, "wsop.capture.proof.title")}</span>
            <MuiTextField
              fullWidth
              size="small"
              className="qtext"
              value={p.title}
              onChange={(e) => {
                const title = e.target.value;
                updateCounted(p.id, { title, key: keyForTitle(title, p.key, savedKeys, new Set(rows.lumpSumProofs.map((x) => x.key)), "capture") });
              }}
            />
          </label>
          <label className="numfield">
            <span className="numlbl">{copy(pc, "wsop.capture.proof.hint")}</span>
            <MuiTextField fullWidth size="small" value={p.hint} onChange={(e) => updateCounted(p.id, { hint: e.target.value })} />
          </label>
          {!p.key ? (
            <label className="numfield">
              <span className="numlbl">{copy(pc, "inspection.question.key")}</span>
              <MuiTextField fullWidth size="small" value={p.key} onChange={(e) => updateCounted(p.id, { key: e.target.value })} />
            </label>
          ) : null}
          <div className="rowf">
            <label className="numfield">
              <span className="numlbl">{copy(pc, "wsop.capture.lump_sum.slot_min")}</span>
              <MuiTextField size="small" slotProps={{ htmlInput: { inputMode: "numeric" } }} value={p.min} onChange={(e) => updateCounted(p.id, { min: e.target.value })} />
            </label>
            <label className="numfield">
              <span className="numlbl">
                {copy(pc, "wsop.capture.lump_sum.slot_max")} (≤ {LUMP_SUM_VIDEO_CEILING})
              </span>
              <MuiTextField size="small" slotProps={{ htmlInput: { inputMode: "numeric" } }} value={p.max} onChange={(e) => updateCounted(p.id, { max: e.target.value })} />
            </label>
          </div>
        </div>
      </div>
    );
  }

  function submit(publish: boolean) {
    const doc = emitWeighing(rows);
    startTransition(async () => {
      const res = publish ? await publishWeighingVersion(sopId, doc) : await saveWeighingVersion(sopId, doc);
      setResult(res);
      if (res.ok && publish) {
        router.push(publishedHref(basePath, sopId, res.versionNumber));
        router.refresh();
      }
    });
  }

  const removalOn = rows.removalMode !== "off";

  return (
    <Box className="kit-enter screen on sop-kit sop-inspection sop-weighing" sx={inspectionEditorSx}>
      <EditorHeader
        crumbs={[copy(pc, "crumb"), pc.title, sopName]}
        title={copy(pc, "wsop.title")}
        subtitle={copy(pc, "wsop.subtitle")}
        version={versionLabel}
        notice={copy(pc, "wsop.notice.pinned")}
        backHref={basePath}
        actions={
          <StudioViewToggle
            label={copy(pc, "studio.view.label")}
            listLabel={copy(pc, "studio.view.list")}
            flowLabel={copy(pc, "studio.view.flow")}
            value={view}
            onChange={switchView}
          />
        }
      />

      {result ? (
        <Alert severity={result.ok ? "info" : "warning"} style={{ marginBottom: 12 }} role="status">
          {result.ok ? <Tag tone="ok">{copy(pc, "modal.builder.notice_ok")}</Tag> : <AlertTriangle className="ic" />} {result.message}
          {result.report && !result.report.valid ? (
            <ul className="small" style={{ margin: "6px 0 0 16px" }}>
              {result.report.errors.slice(0, 6).map((e, i) => (
                <li key={i}>{e.message}</li>
              ))}
            </ul>
          ) : null}
        </Alert>
      ) : null}

      {view === "flow" ? (
        <WeighingFlow pc={pc} rows={rows} proofKindLabels={proofKindLabels} selected={selectedRef} onSelect={setSelectedRef} onInsert={insertAt} renderCard={cardFor} />
      ) : null}
      {view === "flow" ? null : (
      <>
      {/* 1. Planning */}
      <div>
      <Card className="card inspection-page">
        <div className="inspection-page-head" style={{ cursor: "default" }}>
          <span className="qnum">1</span>
          <strong>{copy(pc, "wsop.section.planning")}</strong>
          <span className="muted small">{copy(pc, "wsop.section.planning.subtitle")}</span>
        </div>
        <div className="bd">
          <div className="qcfg">
            <div className="qcfg-head">
              <span className="qcfg-title">{copy(pc, "wsop.planning.modes")}</span>
            </div>
            {WEIGHING_MODES.map((mode) => (
              <FormControlLabel key={mode} className="chkline" control={<Checkbox checked={rows.modes.includes(mode)} onChange={(e) => toggleMode(mode, e.target.checked)} sx={{ p: { xs: 1.5, sm: 1 } }} />} label={<>{copy(pc, `wsop.planning.mode.${mode}`)}</>} />
            ))}
            <div className="rowf" style={{ marginTop: 8 }}>
              <label className="numfield">
                <span className="numlbl">{copy(pc, "wsop.planning.default_cap")}</span>
                <MuiTextField size="small" slotProps={{ htmlInput: { inputMode: "numeric" } }} value={rows.defaultCapPerDay} onChange={(e) => setRows((r) => ({ ...r, defaultCapPerDay: e.target.value }))} />
              </label>
            </div>
          </div>
        </div>
      </Card>
      </div>

      {/* 2. Feed & water removal */}
      <div>
      <Card className="card inspection-page">
        <div className="inspection-page-head" style={{ cursor: "default" }}>
          <span className="qnum">2</span>
          <strong>{copy(pc, "wsop.section.removal")}</strong>
          <span className="muted small">{copy(pc, "wsop.section.removal.subtitle")}</span>
        </div>
        <div className="bd">
          <div className="qcfg">
            <div className="qcfg-head">
              <span className="qcfg-title">{copy(pc, "wsop.removal.mode")}</span>
            </div>
            {removalModes.map((m) => (
              <FormControlLabel key={m.key} className="chkline" control={<Radio name="wsop-removal-mode" value={m.key} checked={rows.removalMode === m.key} onChange={() => setRows((r) => ({ ...r, removalMode: m.key as RemovalMode }))} sx={{ p: { xs: 1.5, sm: 1 } }} />} label={<>{" "}
                <b>{m.label}</b>
                {m.title ? <span className="muted small"> — {m.title}</span> : null}</>} />
            ))}
          </div>
          {removalOn ? (
            <>
              <div className="qcfg" style={{ marginTop: 10 }}>
                <label className="numfield">
                  <span className="numlbl">{copy(pc, "wsop.removal.instruction")}</span>
                  <textarea className="qhelp" rows={3} value={rows.removalInstruction} onChange={(e) => setRows((r) => ({ ...r, removalInstruction: e.target.value }))} />
                </label>
                <div className="rowf" style={{ marginTop: 8 }}>
                  <label className="numfield">
                    <span className="numlbl">{copy(pc, "wsop.removal.cutoff")}</span>
                    <MuiTextField size="small" type="time" value={rows.removalCutoffTime} onChange={(e) => setRows((r) => ({ ...r, removalCutoffTime: e.target.value }))} />
                    <span className="muted small">{copy(pc, "wsop.removal.cutoff.hint")}</span>
                  </label>
                </div>
              </div>
              <div className="qcfg" style={{ marginTop: 10 }}>
                <div className="qcfg-head">
                  <span className="qcfg-title">{copy(pc, "wsop.removal.proofs")}</span>
                  <span className="muted small">{copy(pc, "wsop.removal.proofs.subtitle")}</span>
                </div>
                <div className="qlist">
                  {rows.removalProofs.map((p, i) => slotCard("removalProofs", p, i, { title: "wsop.removal.proof.title", hint: "wsop.removal.proof.hint", remove: "wsop.removal.proof.remove" }))}
                  <Button color="primary" variant="text" size="small" startIcon={<Plus size={14} />} onClick={() => addSlot("removalProofs")}>
            {copy(pc, "wsop.removal.proof.add")}
          </Button>
                </div>
              </div>
              <div className="qcfg" style={{ marginTop: 10 }}>
                <div className="qcfg-head">
                  <span className="qcfg-title">{copy(pc, "wsop.removal.questions")}</span>
                  <span className="muted small">{copy(pc, "wsop.removal.questions.subtitle")}</span>
                </div>
                {questionList("removalQuestions", "wsop.removal.questions.empty")}
              </div>
            </>
          ) : (
            <p className="muted small" style={{ marginTop: 8 }}>
              {copy(pc, "wsop.removal.off_note")}
            </p>
          )}
        </div>
      </Card>
      </div>

      {/* 3. Capture */}
      <div>
      <Card className="card inspection-page">
        <div className="inspection-page-head" style={{ cursor: "default" }}>
          <span className="qnum">3</span>
          <strong>{copy(pc, "wsop.section.capture")}</strong>
          <span className="muted small">{copy(pc, "wsop.section.capture.subtitle")}</span>
        </div>
        <div className="bd">
          {/* 3a. Per animal */}
          <div className="qcfg">
            <div className="qcfg-head">
              <span className="qcfg-title">{copy(pc, "wsop.capture.individual.title")}</span>
              <span className="muted small">{copy(pc, "wsop.capture.individual.subtitle")}</span>
            </div>
            <FormControlLabel className="chkline" disabled control={<Checkbox checked={rows.individualVideoRequired} sx={{ p: { xs: 1.5, sm: 1 } }} />} label={<><span>
                {copy(pc, "wsop.capture.individual.video")}{" "}
                <span className="muted small">
                  <Lock className="ic" style={{ width: 12 }} /> {copy(pc, "wsop.capture.individual.locked_short")}
                </span>
              </span></>} />
            <div className="qcfg" style={{ marginTop: 10 }}>
              <div className="qcfg-head">
                <span className="qcfg-title">{copy(pc, "wsop.capture.individual.proofs")}</span>
                <span className="muted small">{copy(pc, "wsop.capture.individual.proofs.subtitle")}</span>
              </div>
              <div className="qlist">
                {rows.individualProofs.map((p, i) => slotCard("individualProofs", p, i, { title: "wsop.capture.proof.title", hint: "wsop.capture.proof.hint", remove: "wsop.capture.proof.remove" }))}
                <Button color="primary" variant="text" size="small" startIcon={<Plus size={14} />} onClick={() => addSlot("individualProofs")}>
            {copy(pc, "wsop.capture.individual.add_capture")}
          </Button>
              </div>
              <p className="muted small" style={{ marginTop: 4 }}>
                {copy(pc, "wsop.capture.individual.at_least_one")}
              </p>
            </div>
            <div className="qcfg" style={{ marginTop: 10 }}>
              <div className="qcfg-head">
                <span className="qcfg-title">{copy(pc, "wsop.capture.individual.questions")}</span>
                <span className="muted small">{copy(pc, "wsop.capture.individual.questions.subtitle")}</span>
              </div>
              {questionList("individualQuestions", "wsop.capture.individual.questions.empty")}
            </div>
          </div>

          {/* 3b. Whole pen */}
          <div className="qcfg" style={{ marginTop: 14 }}>
            <div className="qcfg-head">
              <span className="qcfg-title">{copy(pc, "wsop.capture.lump_sum.title")}</span>
              <span className="muted small">{copy(pc, "wsop.capture.lump_sum.subtitle")}</span>
            </div>
            <div className="qcfg" style={{ marginTop: 10 }}>
              <div className="qcfg-head">
                <span className="qcfg-title">{copy(pc, "wsop.capture.lump_sum.proofs")}</span>
                <span className="muted small">{copy(pc, "wsop.capture.lump_sum.proofs.subtitle")}</span>
              </div>
              <div className="qlist">
                {rows.lumpSumProofs.map((p, i) => countedCard(p, i))}
                <Button color="primary" variant="text" size="small" startIcon={<Plus size={14} />} onClick={addCounted}>
                  {copy(pc, "wsop.capture.lump_sum.add_capture")}
                </Button>
              </div>
              <p className="muted small" style={{ marginTop: 4 }}>
                {copy(pc, "wsop.capture.lump_sum.total_ceiling")} · {fillCopy(copy(pc, "wsop.summary.lump_sum_videos"), { min: lumpWindow.min, max: lumpWindow.max })}
              </p>
            </div>
            <div className="qcfg" style={{ marginTop: 10 }}>
              <div className="qcfg-head">
                <span className="qcfg-title">{copy(pc, "wsop.capture.lump_sum.questions")}</span>
                <span className="muted small">{copy(pc, "wsop.capture.lump_sum.questions.subtitle")}</span>
              </div>
              {questionList("lumpSumQuestions", "wsop.capture.lump_sum.questions.empty")}
            </div>
          </div>

          <p className="muted small" style={{ marginTop: 8 }}>
            {copy(pc, "wsop.capture.locked_rules")}
          </p>
        </div>
      </Card>
      </div>

      </>
      )}
      <StickyActions>
        <div>
          {problems.length ? (
            <ul className="small muted" style={{ margin: "4px 0 0 16px" }}>
              {problems.slice(0, 5).map((p, i) => (
                <li key={i}>{p}</li>
              ))}
            </ul>
          ) : (
            <span className="muted small">{copy(pc, "wsop.footer.ready")}</span>
          )}
        </div>
        <span className="spacer" style={{ flex: 1 }} />
        <Button color="primary" variant="outlined" loading={pending} disabled={pending || problems.length > 0} onClick={() => submit(false)}>
          {copy(pc, "inspection.action.save_draft")}
        </Button>
        <Button variant="contained" color="primary" startIcon={<Check size={16} />} disabled={pending || problems.length > 0} onClick={() => submit(true)}>
          {copy(pc, "inspection.action.publish")}
        </Button>
      </StickyActions>
    </Box>
  );
}

function fillCopy(template: string, vars: Record<string, string | number>): string {
  return Object.entries(vars).reduce((t, [k, v]) => t.split(`{${k}}`).join(String(v)), template);
}

export function QuestionCard({
  pc, index, count, q, kinds, earlier, takenKeys, savedKeys, onChange, onOptionRenamed, onMove, onRemove,
}: {
  pc: AdminUiPageContract;
  index: number;
  count: number;
  q: WeighingQuestionRow;
  kinds: { key: string; label: string; title?: string }[];
  earlier: WeighingQuestionRow[];
  takenKeys: Set<string>;
  /** Keys the loaded version carries; any other question id follows its title (keyForTitle). */
  savedKeys: Set<string>;
  onChange: (patch: Partial<WeighingQuestionRow>) => void;
  onOptionRenamed: (oldValue: string, newValue: string) => void;
  onMove: (dir: -1 | 1) => void;
  onRemove: () => void;
}) {
  const dep = earlier.find((e) => e.key === q.onlyIfQuestion);
  return (
    <div className="qcard">
      <div className="qhead">
        <span className="qnum">{index + 1}</span>
        <span className="qtype">
          <InlineSelect
            label={copy(pc, "inspection.question.kind")}
            value={q.kind}
            minWidth={168}
            options={kinds.map((k) => ({ value: k.key, label: k.label }))}
            onChange={(next) => {
              const kind = next as WeighingQuestionKind;
              onChange({
                kind,
                allowOther: kind === "choice" && q.allowOther,
                options: (kind === "choice" || kind === "multi") && q.options.length === 0
                  ? [{ value: "yes", label: copy(pc, "option.yes") }, { value: "no", label: copy(pc, "option.no") }]
                  : q.options,
              });
            }}
          />
        </span>
        <span className="sp" style={{ flex: 1 }} />
        <IconButton type="button" size="small" className="ia" aria-label={copy(pc, "inspection.question.move_up")} disabled={index === 0} onClick={() => onMove(-1)}>
          <ChevronUp className="ic" />
        </IconButton>
        <IconButton type="button" size="small" className="ia" aria-label={copy(pc, "inspection.question.move_down")} disabled={index === count - 1} onClick={() => onMove(1)}>
          <ChevronDown className="ic" />
        </IconButton>
        <IconButton type="button" size="small" className="ia del" aria-label={copy(pc, "inspection.question.remove")} onClick={onRemove}>
          <X className="ic" />
        </IconButton>
      </div>
      <div className="qbody">
        <label className="numfield">
          <span className="numlbl">{copy(pc, "wsop.question.title")}</span>
          <MuiTextField
            fullWidth
            size="small"
            className="qtext"
            value={q.title}
            onChange={(e) => {
              const title = e.target.value;
              onChange({ title, key: keyForTitle(title, q.key, savedKeys, takenKeys) });
            }}
          />
        </label>
        <label className="numfield">
          <span className="numlbl">{copy(pc, "inspection.question.hint")}</span>
          <textarea className="qhelp" rows={2} value={q.hint} onChange={(e) => onChange({ hint: e.target.value })} />
        </label>
        {!q.key ? (
          <label className="numfield">
            <span className="numlbl">{copy(pc, "inspection.question.key")}</span>
            <MuiTextField fullWidth size="small" value={q.key} onChange={(e) => onChange({ key: e.target.value })} />
          </label>
        ) : null}

        {q.kind === "choice" || q.kind === "multi" ? (
          <div className="qcfg">
            <div className="qcfg-head">
              <span className="qcfg-title">{copy(pc, "inspection.question.options")}</span>
              <Button color="primary" variant="text" size="small" startIcon={<Plus size={14} />} onClick={() => onChange({ options: [...q.options, { value: "", label: "" }] })}>
                {copy(pc, "inspection.question.add_option")}
              </Button>
            </div>
            {q.options.map((o, i) => (
              <div className="optrow" key={i}>
                <span className="optmark">{q.kind === "choice" ? <span className="optdot" /> : <span className="optbox" />}</span>
                <MuiTextField
                  fullWidth
                  size="small"
                  value={o.label}
                  onChange={(e) => {
                    const label = e.target.value;
                    // The wire value follows the label (unique within the question); the "other"
                    // value is kept: it is what attaches the free text.
                    const others = new Set(q.options.filter((_, j) => j !== i).map((y) => y.value));
                    const value = o.value === "other" ? "other" : slugKey(label, others, "choice");
                    const options = q.options.map((x, j) => (j === i ? { label, value } : x));
                    onChange({ options });
                    if (o.value && o.value !== value) onOptionRenamed(o.value, value);
                  }}
                />
                <code className="muted small">{o.value}</code>
                <IconButton type="button" size="small" className="ia del" aria-label={copy(pc, "wsop.question.remove_choice")} onClick={() => onChange({ options: q.options.filter((_, j) => j !== i) })}>
                  <X className="ic" />
                </IconButton>
              </div>
            ))}
            {q.kind === "choice" ? (
              <FormControlLabel className="chkline" control={<Checkbox checked={q.allowOther} onChange={(e) => onChange({ allowOther: e.target.checked })} sx={{ p: { xs: 1.5, sm: 1 } }} />} label={<>{copy(pc, "inspection.question.allow_other")}</>} />
            ) : null}
          </div>
        ) : null}

        {q.kind === "number" ? (
          <div className="qcfg">
            <div className="rowf">
              <label className="numfield">
                <span className="numlbl">{copy(pc, "inspection.question.min")}</span>
                <MuiTextField size="small" value={q.min} slotProps={{ htmlInput: { inputMode: "decimal" } }} onChange={(e) => onChange({ min: e.target.value })} />
              </label>
              <label className="numfield">
                <span className="numlbl">{copy(pc, "inspection.question.max")}</span>
                <MuiTextField size="small" value={q.max} slotProps={{ htmlInput: { inputMode: "decimal" } }} onChange={(e) => onChange({ max: e.target.value })} />
              </label>
              <label className="numfield">
                <span className="numlbl">{copy(pc, "inspection.question.unit")}</span>
                <MuiTextField size="small" value={q.unit} onChange={(e) => onChange({ unit: e.target.value })} />
              </label>
            </div>
          </div>
        ) : null}

        <div className="qfoot">
          <FormControlLabel className="chkline" control={<Checkbox checked={q.required} onChange={(e) => onChange({ required: e.target.checked })} sx={{ p: { xs: 1.5, sm: 1 } }} />} label={<>{copy(pc, "inspection.question.required")}</>} />
          <span className="condrow">
            {copy(pc, "inspection.question.only_if")}
            <InlineSelect
              label={copy(pc, "inspection.question.only_if")}
              value={q.onlyIfQuestion}
              options={[
                { value: "", label: copy(pc, "inspection.question.always") },
                ...earlier.filter((e) => e.kind === "choice").map((e) => ({ value: e.key, label: e.title || e.key })),
              ]}
              onChange={(next) => onChange({ onlyIfQuestion: next, onlyIfValue: "" })}
            />
            {dep ? (
              <>
                {copy(pc, "inspection.question.only_if_value")}
                <InlineSelect
                  label={copy(pc, "inspection.question.only_if_value")}
                  value={q.onlyIfValue}
                  minWidth={120}
                  options={[{ value: "", label: "—" }, ...dep.options.map((o) => ({ value: o.value, label: o.label }))]}
                  onChange={(next) => onChange({ onlyIfValue: next })}
                />
              </>
            ) : null}
          </span>
        </div>
      </div>
    </div>
  );
}
