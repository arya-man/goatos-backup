"use client";

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
import Link from "@/components/no-prefetch-link";
import { useRouter } from "next/navigation";
import { AlertTriangle, Check, ChevronDown, ChevronLeft, ChevronUp, Lock, Plus, X } from "lucide-react";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
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
import { publishWeighingVersion, saveWeighingVersion, type WeighingSaveResult } from "./sop-actions";

type Props = {
  pageContract: AdminUiPageContract;
  basePath: string;
  sopId: string;
  sopName: string;
  sopCode: string;
  versionLabel: string;
  initial: WeighingRows;
};

export function WeighingEditor({ pageContract: pc, basePath, sopId, sopName, sopCode, versionLabel, initial }: Props) {
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
  const kinds = optionGroup(pc, "wsop_question_kinds");
  const removalModes = optionGroup(pc, "wsop_removal_modes");
  const proofKinds = optionGroup(pc, "wsop_proof_kinds");
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
        <button type="button" className="btn sm ghost" onClick={() => addQuestion(list)}>
          <Plus className="ic" /> {copy(pc, "inspection.question.add")}
        </button>
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
            <select value={p.kind} onChange={(e) => updateSlot(list, p.id, { kind: e.target.value as RemovalProofKind })}>
              {proofKinds.map((k) => (
                <option key={k.key} value={k.key} title={k.title}>
                  {k.label}
                </option>
              ))}
            </select>
          </span>
          {p.key ? <code className="muted small">{p.key}</code> : null}
          <span className="sp" style={{ flex: 1 }} />
          <button type="button" className="ia" aria-label={copy(pc, "inspection.question.move_up")} disabled={i === 0} onClick={() => moveSlot(list, p.id, -1)}>
            <ChevronUp className="ic" />
          </button>
          <button type="button" className="ia" aria-label={copy(pc, "inspection.question.move_down")} disabled={i === slots.length - 1} onClick={() => moveSlot(list, p.id, 1)}>
            <ChevronDown className="ic" />
          </button>
          <button type="button" className="ia del" aria-label={copy(pc, labels.remove)} onClick={() => removeSlot(list, p.id)}>
            <X className="ic" />
          </button>
        </div>
        <div className="qbody">
          <label className="numfield">
            <span className="numlbl">{copy(pc, labels.title)}</span>
            <input
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
            <input value={p.hint} onChange={(e) => updateSlot(list, p.id, { hint: e.target.value })} />
          </label>
          {!p.key ? (
            <label className="numfield">
              <span className="numlbl">{copy(pc, "inspection.question.key")}</span>
              <input value={p.key} onChange={(e) => updateSlot(list, p.id, { key: e.target.value })} />
            </label>
          ) : null}
          <div className="qfoot">
            <label className="chkline">
              <input type="checkbox" checked={p.required} onChange={(e) => updateSlot(list, p.id, { required: e.target.checked })} /> {copy(pc, "inspection.question.required")}
            </label>
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
            <select value={p.kind} onChange={(e) => updateCounted(p.id, { kind: e.target.value as RemovalProofKind })}>
              {proofKinds.map((k) => (
                <option key={k.key} value={k.key} title={k.title}>
                  {k.label}
                </option>
              ))}
            </select>
          </span>
          {p.key ? <code className="muted small">{p.key}</code> : null}
          <span className="sp" style={{ flex: 1 }} />
          <button type="button" className="ia" aria-label={copy(pc, "inspection.question.move_up")} disabled={i === 0} onClick={() => moveCounted(p.id, -1)}>
            <ChevronUp className="ic" />
          </button>
          <button type="button" className="ia" aria-label={copy(pc, "inspection.question.move_down")} disabled={i === rows.lumpSumProofs.length - 1} onClick={() => moveCounted(p.id, 1)}>
            <ChevronDown className="ic" />
          </button>
          <button type="button" className="ia del" aria-label={copy(pc, "wsop.capture.proof.remove")} onClick={() => removeCounted(p.id)}>
            <X className="ic" />
          </button>
        </div>
        <div className="qbody">
          <label className="numfield">
            <span className="numlbl">{copy(pc, "wsop.capture.proof.title")}</span>
            <input
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
            <input value={p.hint} onChange={(e) => updateCounted(p.id, { hint: e.target.value })} />
          </label>
          {!p.key ? (
            <label className="numfield">
              <span className="numlbl">{copy(pc, "inspection.question.key")}</span>
              <input value={p.key} onChange={(e) => updateCounted(p.id, { key: e.target.value })} />
            </label>
          ) : null}
          <div className="rowf">
            <label className="numfield">
              <span className="numlbl">{copy(pc, "wsop.capture.lump_sum.slot_min")}</span>
              <input inputMode="numeric" value={p.min} onChange={(e) => updateCounted(p.id, { min: e.target.value })} />
            </label>
            <label className="numfield">
              <span className="numlbl">
                {copy(pc, "wsop.capture.lump_sum.slot_max")} (≤ {LUMP_SUM_VIDEO_CEILING})
              </span>
              <input inputMode="numeric" value={p.max} onChange={(e) => updateCounted(p.id, { max: e.target.value })} />
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
    <div className="screen on sop-inspection sop-weighing">
      <div className="phead">
        <div>
          <div className="crumb">
            {copy(pc, "crumb")} · {pc.title} · <b>{sopName}</b>
          </div>
          <h1>{copy(pc, "wsop.title")}</h1>
          <div className="sub">{copy(pc, "wsop.subtitle")}</div>
          <div className="muted small" style={{ marginTop: 4 }}>
            <code>{sopCode}</code> · {versionLabel} · {copy(pc, "wsop.notice.pinned")}
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Link className="btn" href={basePath}>
          <ChevronLeft className="ic" /> {copy(pc, "builder.back")}
        </Link>
      </div>

      {result ? (
        <div className={result.ok ? "note" : "alert warn"} style={{ marginBottom: 12 }}>
          {result.ok ? <span className="tag t-ok">{copy(pc, "modal.builder.notice_ok")}</span> : <AlertTriangle className="ic" />} {result.message}
          {result.report && !result.report.valid ? (
            <ul className="small" style={{ margin: "6px 0 0 16px" }}>
              {result.report.errors.slice(0, 6).map((e, i) => (
                <li key={i}>{e.message}</li>
              ))}
            </ul>
          ) : null}
        </div>
      ) : null}

      {/* 1. Planning */}
      <section className="card inspection-page">
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
              <label className="chkline" key={mode}>
                <input type="checkbox" checked={rows.modes.includes(mode)} onChange={(e) => toggleMode(mode, e.target.checked)} /> {copy(pc, `wsop.planning.mode.${mode}`)}
              </label>
            ))}
            <div className="rowf" style={{ marginTop: 8 }}>
              <label className="numfield">
                <span className="numlbl">{copy(pc, "wsop.planning.default_cap")}</span>
                <input inputMode="numeric" value={rows.defaultCapPerDay} onChange={(e) => setRows((r) => ({ ...r, defaultCapPerDay: e.target.value }))} />
              </label>
            </div>
          </div>
        </div>
      </section>

      {/* 2. Feed & water removal */}
      <section className="card inspection-page">
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
              <label className="chkline" key={m.key}>
                <input type="radio" name="wsop-removal-mode" value={m.key} checked={rows.removalMode === m.key} onChange={() => setRows((r) => ({ ...r, removalMode: m.key as RemovalMode }))} />{" "}
                <b>{m.label}</b>
                {m.title ? <span className="muted small"> — {m.title}</span> : null}
              </label>
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
                    <input type="time" value={rows.removalCutoffTime} onChange={(e) => setRows((r) => ({ ...r, removalCutoffTime: e.target.value }))} />
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
                  <button type="button" className="btn sm ghost" onClick={() => addSlot("removalProofs")}>
                    <Plus className="ic" /> {copy(pc, "wsop.removal.proof.add")}
                  </button>
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
      </section>

      {/* 3. Capture */}
      <section className="card inspection-page">
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
            <label className="chkline" title={copy(pc, "wsop.capture.individual.locked")}>
              <input type="checkbox" checked={rows.individualVideoRequired} disabled />
              <span>
                {copy(pc, "wsop.capture.individual.video")}{" "}
                <span className="muted small">
                  <Lock className="ic" style={{ width: 12 }} /> {copy(pc, "wsop.capture.individual.locked_short")}
                </span>
              </span>
            </label>
            <div className="qcfg" style={{ marginTop: 10 }}>
              <div className="qcfg-head">
                <span className="qcfg-title">{copy(pc, "wsop.capture.individual.proofs")}</span>
                <span className="muted small">{copy(pc, "wsop.capture.individual.proofs.subtitle")}</span>
              </div>
              <div className="qlist">
                {rows.individualProofs.map((p, i) => slotCard("individualProofs", p, i, { title: "wsop.capture.proof.title", hint: "wsop.capture.proof.hint", remove: "wsop.capture.proof.remove" }))}
                <button type="button" className="btn sm ghost" onClick={() => addSlot("individualProofs")}>
                  <Plus className="ic" /> {copy(pc, "wsop.capture.individual.add_capture")}
                </button>
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
                <button type="button" className="btn sm ghost" onClick={addCounted}>
                  <Plus className="ic" /> {copy(pc, "wsop.capture.lump_sum.add_capture")}
                </button>
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
      </section>

      <div className="cfgmf inspection-footer">
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
        <div className="sp" style={{ flex: 1 }} />
        <button type="button" className="btn" disabled={pending || problems.length > 0} onClick={() => submit(false)}>
          {copy(pc, "inspection.action.save_draft")}
        </button>
        <button type="button" className="btn p" disabled={pending || problems.length > 0} onClick={() => submit(true)}>
          <Check className="ic" /> {copy(pc, "inspection.action.publish")}
        </button>
      </div>
    </div>
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
          <select
            value={q.kind}
            onChange={(e) => {
              const kind = e.target.value as WeighingQuestionKind;
              onChange({
                kind,
                allowOther: kind === "choice" && q.allowOther,
                options: (kind === "choice" || kind === "multi") && q.options.length === 0
                  ? [{ value: "yes", label: copy(pc, "option.yes") }, { value: "no", label: copy(pc, "option.no") }]
                  : q.options,
              });
            }}
          >
            {kinds.map((k) => (
              <option key={k.key} value={k.key} title={k.title}>
                {k.label}
              </option>
            ))}
          </select>
        </span>
        <span className="sp" style={{ flex: 1 }} />
        <button type="button" className="ia" aria-label={copy(pc, "inspection.question.move_up")} disabled={index === 0} onClick={() => onMove(-1)}>
          <ChevronUp className="ic" />
        </button>
        <button type="button" className="ia" aria-label={copy(pc, "inspection.question.move_down")} disabled={index === count - 1} onClick={() => onMove(1)}>
          <ChevronDown className="ic" />
        </button>
        <button type="button" className="ia del" aria-label={copy(pc, "inspection.question.remove")} onClick={onRemove}>
          <X className="ic" />
        </button>
      </div>
      <div className="qbody">
        <label className="numfield">
          <span className="numlbl">{copy(pc, "wsop.question.title")}</span>
          <input
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
            <input value={q.key} onChange={(e) => onChange({ key: e.target.value })} />
          </label>
        ) : null}

        {q.kind === "choice" || q.kind === "multi" ? (
          <div className="qcfg">
            <div className="qcfg-head">
              <span className="qcfg-title">{copy(pc, "inspection.question.options")}</span>
              <button type="button" className="btn sm ghost" onClick={() => onChange({ options: [...q.options, { value: "", label: "" }] })}>
                <Plus className="ic" /> {copy(pc, "inspection.question.add_option")}
              </button>
            </div>
            {q.options.map((o, i) => (
              <div className="optrow" key={i}>
                <span className="optmark">{q.kind === "choice" ? <span className="optdot" /> : <span className="optbox" />}</span>
                <input
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
                <button type="button" className="ia del" aria-label={copy(pc, "wsop.question.remove_choice")} onClick={() => onChange({ options: q.options.filter((_, j) => j !== i) })}>
                  <X className="ic" />
                </button>
              </div>
            ))}
            {q.kind === "choice" ? (
              <label className="chkline">
                <input type="checkbox" checked={q.allowOther} onChange={(e) => onChange({ allowOther: e.target.checked })} /> {copy(pc, "inspection.question.allow_other")}
              </label>
            ) : null}
          </div>
        ) : null}

        {q.kind === "number" ? (
          <div className="qcfg">
            <div className="rowf">
              <label className="numfield">
                <span className="numlbl">{copy(pc, "inspection.question.min")}</span>
                <input value={q.min} inputMode="decimal" onChange={(e) => onChange({ min: e.target.value })} />
              </label>
              <label className="numfield">
                <span className="numlbl">{copy(pc, "inspection.question.max")}</span>
                <input value={q.max} inputMode="decimal" onChange={(e) => onChange({ max: e.target.value })} />
              </label>
              <label className="numfield">
                <span className="numlbl">{copy(pc, "inspection.question.unit")}</span>
                <input value={q.unit} onChange={(e) => onChange({ unit: e.target.value })} />
              </label>
            </div>
          </div>
        ) : null}

        <div className="qfoot">
          <label className="chkline">
            <input type="checkbox" checked={q.required} onChange={(e) => onChange({ required: e.target.checked })} /> {copy(pc, "inspection.question.required")}
          </label>
          <span className="condrow">
            {copy(pc, "inspection.question.only_if")}
            <select value={q.onlyIfQuestion} onChange={(e) => onChange({ onlyIfQuestion: e.target.value, onlyIfValue: "" })}>
              <option value="">{copy(pc, "inspection.question.always")}</option>
              {earlier.filter((e) => e.kind === "choice").map((e) => (
                <option key={e.key} value={e.key}>
                  {e.title || e.key}
                </option>
              ))}
            </select>
            {dep ? (
              <>
                {copy(pc, "inspection.question.only_if_value")}
                <select className="condval" value={q.onlyIfValue} onChange={(e) => onChange({ onlyIfValue: e.target.value })}>
                  <option value="">—</option>
                  {dep.options.map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </select>
              </>
            ) : null}
          </span>
        </div>
      </div>
    </div>
  );
}
