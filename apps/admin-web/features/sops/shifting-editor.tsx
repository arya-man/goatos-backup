"use client";

// SHIFTING SOP (maintainer decision 2026-09-16, docs/decisions/shifting-sop.md).
//
// The shifting cards editor: the RAISE extras (questions and optional captures on the raise form,
// seen by the park head before approval), the COMPLETION card (the captures and questions every
// completion carries) and the HIGH-PRIORITY card (added to a high movement's completion). Every
// word on this screen is the backend contract's; the backend validates the document on save and
// refuses one the phone could not render, naming the field. Park head approval before completion,
// the Feed Config fingerprint on a high-priority movement, the atomic herd move and verifier review
// are maintainer locks and are shown, not edited.
import { useMemo, useState, useTransition } from "react";
import Link from "@/components/no-prefetch-link";
import { useRouter } from "next/navigation";
import { AlertTriangle, Check, ChevronDown, ChevronLeft, ChevronUp, Lock, Plus, X } from "lucide-react";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { blankProofSlot, blankQuestion, followQuestionKey, keyForTitle, type RemovalProofKind, type RemovalProofRow, type WeighingQuestionRow } from "./weighing-model";
import { QuestionCard } from "./weighing-editor";
import { SHIFTING_SECTIONS, emitShifting, shiftingProblems, type ShiftingRows, type ShiftingSection, type ShiftingSectionRows } from "./shifting-model";
import { publishedHref } from "./published-href";
import { publishShiftingVersion, saveShiftingVersion, type ShiftingSaveResult } from "./sop-actions";

type Props = {
  pageContract: AdminUiPageContract;
  basePath: string;
  sopId: string;
  sopName: string;
  sopCode: string;
  versionLabel: string;
  initial: ShiftingRows;
};

export function ShiftingEditor({ pageContract: pc, basePath, sopId, sopName, sopCode, versionLabel, initial }: Props) {
  const router = useRouter();
  const [rows, setRows] = useState<ShiftingRows>(initial);
  // Keys the loaded version already carries never move; a new capture / question follows its title.
  const [savedKeys] = useState<Set<string>>(
    () => new Set(SHIFTING_SECTIONS.flatMap((s) => [...initial[s].proofs, ...initial[s].questions].map((x) => x.key)).filter(Boolean)),
  );
  const [result, setResult] = useState<ShiftingSaveResult | null>(null);
  const [pending, startTransition] = useTransition();
  const kinds = optionGroup(pc, "wsop_question_kinds");
  const proofKinds = optionGroup(pc, "wsop_proof_kinds");
  const sectionLabel = (section: ShiftingSection) => copy(pc, `ssop.section.${section}`);
  const problems = useMemo(() => shiftingProblems(rows, sectionLabel), [rows]); // eslint-disable-line react-hooks/exhaustive-deps
  // Every key is one register: a capture key or question key on one card is taken on the others.
  const allCaptureKeys = new Set(SHIFTING_SECTIONS.flatMap((s) => rows[s].proofs.map((p) => p.key)));
  const allQuestionKeys = new Set(SHIFTING_SECTIONS.flatMap((s) => rows[s].questions.map((q) => q.key)));

  function patchSection(section: ShiftingSection, fn: (s: ShiftingSectionRows) => ShiftingSectionRows) {
    setRows((r) => ({ ...r, [section]: fn(r[section]) }));
  }

  function submit(publish: boolean) {
    const doc = emitShifting(rows);
    startTransition(async () => {
      const res = publish ? await publishShiftingVersion(sopId, doc) : await saveShiftingVersion(sopId, doc);
      setResult(res);
      if (res.ok && publish) {
        router.push(publishedHref(basePath, sopId, res.versionNumber));
        router.refresh();
      }
    });
  }

  return (
    <div className="screen on sop-inspection sop-weighing sop-feed sop-shifting">
      <div className="phead">
        <div>
          <div className="crumb">
            {copy(pc, "crumb")} · {pc.title} · <b>{sopName}</b>
          </div>
          <h1>{copy(pc, "ssop.title")}</h1>
          <div className="sub">{copy(pc, "ssop.subtitle")}</div>
          <div className="muted small" style={{ marginTop: 4 }}>
            <code>{sopCode}</code> · {versionLabel} · {copy(pc, "ssop.notice.pinned")}
          </div>
        </div>
        <div className="acts">
          <Link className="btn" href={basePath}>
            <ChevronLeft className="ic" /> {copy(pc, "builder.back")}
          </Link>
        </div>
      </div>

      {result ? (
        <div className={`alert ${result.ok ? "ok" : ""}`} role="status">
          {result.ok ? <Check className="ic" /> : <AlertTriangle className="ic" />}
          <div>{result.message}</div>
        </div>
      ) : null}

      {SHIFTING_SECTIONS.map((section, si) => {
        const block = rows[section];
        return (
          <section className="card inspection-page" key={section}>
            <div className="inspection-page-head" style={{ cursor: "default" }}>
              <span className="qnum">{si + 1}</span>
              <strong>{sectionLabel(section)}</strong>
              <span className="muted small">{copy(pc, `ssop.section.${section}.sub`)}</span>
            </div>
            <div className="bd">
              <div className="qcfg">
                <label className="numfield">
                  <span className="numlbl">{copy(pc, "ssop.instruction")}</span>
                  <textarea className="qhelp" rows={3} value={block.instruction} onChange={(e) => patchSection(section, (s) => ({ ...s, instruction: e.target.value }))} />
                </label>
              </div>

              <div className="qcfg" style={{ marginTop: 10 }}>
                <div className="qcfg-head">
                  <span className="qcfg-title">{copy(pc, "ssop.proofs")}</span>
                  <span className="muted small">{copy(pc, "ssop.proofs.subtitle")}</span>
                </div>
                <div className="qlist">
                  {block.proofs.map((p, i) => (
                    <SlotCard
                      key={p.id}
                      pc={pc}
                      index={i}
                      count={block.proofs.length}
                      slot={p}
                      proofKinds={proofKinds}
                      takenKeys={allCaptureKeys}
                      savedKeys={savedKeys}
                      onChange={(patch) => patchSection(section, (s) => ({ ...s, proofs: s.proofs.map((x) => (x.id === p.id ? { ...x, ...patch } : x)) }))}
                      onMove={(dir) =>
                        patchSection(section, (s) => {
                          const idx = s.proofs.findIndex((x) => x.id === p.id);
                          const j = idx + dir;
                          if (idx < 0 || j < 0 || j >= s.proofs.length) return s;
                          const next = [...s.proofs];
                          [next[idx], next[j]] = [next[j], next[idx]];
                          return { ...s, proofs: next };
                        })
                      }
                      onRemove={() => patchSection(section, (s) => ({ ...s, proofs: s.proofs.filter((x) => x.id !== p.id) }))}
                    />
                  ))}
                  <button type="button" className="btn sm ghost" onClick={() => patchSection(section, (s) => ({ ...s, proofs: [...s.proofs, blankProofSlot()] }))}>
                    <Plus className="ic" /> {copy(pc, "ssop.proof.add")}
                  </button>
                </div>
              </div>

              <div className="qcfg" style={{ marginTop: 10 }}>
                <div className="qcfg-head">
                  <span className="qcfg-title">{copy(pc, "ssop.questions")}</span>
                  <span className="muted small">{copy(pc, "ssop.questions.subtitle")}</span>
                </div>
                <div className="qlist">
                  {block.questions.length === 0 ? <p className="muted">{copy(pc, "ssop.questions.empty")}</p> : null}
                  {block.questions.map((q, qi) => (
                    <QuestionCard
                      key={q.id}
                      pc={pc}
                      index={qi}
                      count={block.questions.length}
                      q={q}
                      kinds={kinds}
                      earlier={block.questions.slice(0, qi)}
                      takenKeys={allQuestionKeys}
                      savedKeys={savedKeys}
                      onChange={(patch: Partial<WeighingQuestionRow>) => patchSection(section, (s) => ({ ...s, questions: followQuestionKey(s.questions, q.id, patch) }))}
                      onOptionRenamed={(from: string, to: string) =>
                        patchSection(section, (s) => ({
                          ...s,
                          questions: s.questions.map((x) => (x.onlyIfQuestion === q.key && x.onlyIfValue === from ? { ...x, onlyIfValue: to } : x)),
                        }))
                      }
                      onMove={(dir: -1 | 1) =>
                        patchSection(section, (s) => {
                          const idx = s.questions.findIndex((x) => x.id === q.id);
                          const j = idx + dir;
                          if (idx < 0 || j < 0 || j >= s.questions.length) return s;
                          const next = [...s.questions];
                          [next[idx], next[j]] = [next[j], next[idx]];
                          return { ...s, questions: next };
                        })
                      }
                      onRemove={() => patchSection(section, (s) => ({ ...s, questions: s.questions.filter((x) => x.id !== q.id) }))}
                    />
                  ))}
                  <button type="button" className="btn sm ghost" onClick={() => patchSection(section, (s) => ({ ...s, questions: [...s.questions, blankQuestion()] }))}>
                    <Plus className="ic" /> {copy(pc, "inspection.question.add")}
                  </button>
                </div>
              </div>
            </div>
          </section>
        );
      })}

      <section className="card inspection-page">
        <div className="bd">
          <p className="muted small" style={{ margin: 0 }}>
            <Lock className="ic" style={{ width: 12 }} /> {copy(pc, "ssop.locked")}
          </p>
          <p className="muted small" style={{ margin: "6px 0 0" }}>
            {copy(pc, "ssop.summary.legacy_key_note")}
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
            <span className="muted small">{copy(pc, "ssop.footer.ready")}</span>
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

function SlotCard({
  pc, index, count, slot, proofKinds, takenKeys, savedKeys, onChange, onMove, onRemove,
}: {
  pc: AdminUiPageContract;
  index: number;
  count: number;
  slot: RemovalProofRow;
  proofKinds: { key: string; label: string; title?: string }[];
  takenKeys: Set<string>;
  /** Keys the loaded version carries; any other key follows its title (keyForTitle). */
  savedKeys: Set<string>;
  onChange: (patch: Partial<RemovalProofRow>) => void;
  onMove: (dir: -1 | 1) => void;
  onRemove: () => void;
}) {
  return (
    <div className="qcard">
      <div className="qhead">
        <span className="qnum">{index + 1}</span>
        <span className="qtype">
          <select value={slot.kind} onChange={(e) => onChange({ kind: e.target.value as RemovalProofKind })}>
            {proofKinds.map((k) => (
              <option key={k.key} value={k.key} title={k.title}>
                {k.label}
              </option>
            ))}
          </select>
        </span>
        {slot.key ? <code className="muted small">{slot.key}</code> : null}
        <span className="sp" style={{ flex: 1 }} />
        <button type="button" className="ia" aria-label={copy(pc, "inspection.question.move_up")} disabled={index === 0} onClick={() => onMove(-1)}>
          <ChevronUp className="ic" />
        </button>
        <button type="button" className="ia" aria-label={copy(pc, "inspection.question.move_down")} disabled={index === count - 1} onClick={() => onMove(1)}>
          <ChevronDown className="ic" />
        </button>
        <button type="button" className="ia del" aria-label={copy(pc, "ssop.proof.remove")} onClick={onRemove}>
          <X className="ic" />
        </button>
      </div>
      <div className="qbody">
        <label className="numfield">
          <span className="numlbl">{copy(pc, "ssop.proof.title")}</span>
          <input
            className="qtext"
            value={slot.title}
            onChange={(e) => {
              const title = e.target.value;
              // A NEW slot's key follows its title until saved; an existing key is never rewritten
              // (it is what the phones stamp on uploads).
              onChange({ title, key: keyForTitle(title, slot.key, savedKeys, takenKeys, "capture") });
            }}
          />
        </label>
        <label className="numfield">
          <span className="numlbl">{copy(pc, "ssop.proof.hint")}</span>
          <input value={slot.hint} onChange={(e) => onChange({ hint: e.target.value })} />
        </label>
        {!slot.key ? (
          <label className="numfield">
            <span className="numlbl">{copy(pc, "inspection.question.key")}</span>
            <input value={slot.key} onChange={(e) => onChange({ key: e.target.value })} />
          </label>
        ) : null}
        <div className="qfoot">
          <label className="chkline">
            <input type="checkbox" checked={slot.required} onChange={(e) => onChange({ required: e.target.checked })} /> {copy(pc, "inspection.question.required")}
          </label>
        </div>
      </div>
    </div>
  );
}
