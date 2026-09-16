"use client";

// FEED SOP (maintainer decision 2026-09-16, docs/decisions/feed-sop.md).
//
// The feed cards editor: for each stage the document carries (distribution + wastage on
// feed.direction, packing on feed.packing, transport on feed.transport) the instruction shown on
// the card, the CAPTURES the crew records -- a live-camera video, a photo or either, compulsory or
// optional, in the order the phone shows them -- and the QUESTIONS they answer. Every word on this
// screen is the backend contract's; the backend validates the document on save and refuses one the
// phone could not render, naming the field. The collaboration model (any operator may record any
// capture, slots are independent), the per-bag packing grain and the one-trip transport grain are
// maintainer locks and are shown, not edited.
import { useMemo, useState, useTransition } from "react";
import Link from "@/components/no-prefetch-link";
import { useRouter } from "next/navigation";
import { AlertTriangle, Check, ChevronDown, ChevronLeft, ChevronUp, Lock, Plus, X } from "lucide-react";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { blankProofSlot, blankQuestion, slugKey, type RemovalProofKind, type RemovalProofRow, type WeighingQuestionRow } from "./weighing-model";
import { QuestionCard } from "./weighing-editor";
import { FEED_STAGES_BY_CODE, emitFeed, feedProblems, type FeedRows, type FeedStage, type FeedStageRows } from "./feed-model";
import { publishedHref } from "./published-href";
import { publishFeedVersion, saveFeedVersion, type FeedSaveResult } from "./sop-actions";

type Props = {
  pageContract: AdminUiPageContract;
  basePath: string;
  sopId: string;
  sopName: string;
  sopCode: string;
  versionLabel: string;
  initial: FeedRows;
};

export function FeedEditor({ pageContract: pc, basePath, sopId, sopName, sopCode, versionLabel, initial }: Props) {
  const router = useRouter();
  const [rows, setRows] = useState<FeedRows>(initial);
  const [result, setResult] = useState<FeedSaveResult | null>(null);
  const [pending, startTransition] = useTransition();
  const kinds = optionGroup(pc, "wsop_question_kinds");
  const proofKinds = optionGroup(pc, "wsop_proof_kinds");
  const stageLabel = (stage: FeedStage) => copy(pc, `fsop.stage.${stage}`);
  const problems = useMemo(() => feedProblems(rows, stageLabel), [rows]); // eslint-disable-line react-hooks/exhaustive-deps
  const stages = FEED_STAGES_BY_CODE[rows.sopCode] ?? [];

  function patchStage(stage: FeedStage, fn: (s: FeedStageRows) => FeedStageRows) {
    setRows((r) => {
      const current = r.stages[stage];
      if (!current) return r;
      return { ...r, stages: { ...r.stages, [stage]: fn(current) } };
    });
  }

  function submit(publish: boolean) {
    const doc = emitFeed(rows);
    startTransition(async () => {
      const res = publish ? await publishFeedVersion(sopId, doc) : await saveFeedVersion(sopId, doc);
      setResult(res);
      if (res.ok && publish) {
        router.push(publishedHref(basePath, sopId, res.versionNumber));
        router.refresh();
      }
    });
  }

  return (
    <div className="screen on sop-inspection sop-weighing sop-feed">
      <div className="phead">
        <div>
          <div className="crumb">
            {copy(pc, "crumb")} · {pc.title} · <b>{sopName}</b>
          </div>
          <h1>{copy(pc, "fsop.title")}</h1>
          <div className="sub">{copy(pc, "fsop.subtitle")}</div>
          <div className="muted small" style={{ marginTop: 4 }}>
            <code>{sopCode}</code> · {versionLabel} · {copy(pc, "fsop.notice.pinned")}
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

      {stages.map((stage, si) => {
        const block = rows.stages[stage];
        if (!block) return null;
        const takenKeys = new Set(block.questions.map((q) => q.key));
        return (
          <section className="card inspection-page" key={stage}>
            <div className="inspection-page-head" style={{ cursor: "default" }}>
              <span className="qnum">{si + 1}</span>
              <strong>{stageLabel(stage)}</strong>
              <span className="muted small">{copy(pc, `fsop.stage.${stage}.sub`)}</span>
            </div>
            <div className="bd">
              <div className="qcfg">
                <label className="numfield">
                  <span className="numlbl">{copy(pc, "fsop.instruction")}</span>
                  <textarea className="qhelp" rows={3} value={block.instruction} onChange={(e) => patchStage(stage, (s) => ({ ...s, instruction: e.target.value }))} />
                </label>
              </div>

              <div className="qcfg" style={{ marginTop: 10 }}>
                <div className="qcfg-head">
                  <span className="qcfg-title">{copy(pc, "fsop.proofs")}</span>
                  <span className="muted small">{copy(pc, "fsop.proofs.subtitle")}</span>
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
                      takenKeys={new Set(block.proofs.map((x) => x.key))}
                      onChange={(patch) => patchStage(stage, (s) => ({ ...s, proofs: s.proofs.map((x) => (x.id === p.id ? { ...x, ...patch } : x)) }))}
                      onMove={(dir) =>
                        patchStage(stage, (s) => {
                          const idx = s.proofs.findIndex((x) => x.id === p.id);
                          const j = idx + dir;
                          if (idx < 0 || j < 0 || j >= s.proofs.length) return s;
                          const next = [...s.proofs];
                          [next[idx], next[j]] = [next[j], next[idx]];
                          return { ...s, proofs: next };
                        })
                      }
                      onRemove={() => patchStage(stage, (s) => ({ ...s, proofs: s.proofs.filter((x) => x.id !== p.id) }))}
                    />
                  ))}
                  <button type="button" className="btn sm ghost" onClick={() => patchStage(stage, (s) => ({ ...s, proofs: [...s.proofs, blankProofSlot()] }))}>
                    <Plus className="ic" /> {copy(pc, "fsop.proof.add")}
                  </button>
                </div>
              </div>

              <div className="qcfg" style={{ marginTop: 10 }}>
                <div className="qcfg-head">
                  <span className="qcfg-title">{copy(pc, "fsop.questions")}</span>
                  <span className="muted small">{copy(pc, "fsop.questions.subtitle")}</span>
                </div>
                <div className="qlist">
                  {block.questions.length === 0 ? <p className="muted">{copy(pc, "fsop.questions.empty")}</p> : null}
                  {block.questions.map((q, qi) => (
                    <QuestionCard
                      key={q.id}
                      pc={pc}
                      index={qi}
                      count={block.questions.length}
                      q={q}
                      kinds={kinds}
                      earlier={block.questions.slice(0, qi)}
                      takenKeys={takenKeys}
                      onChange={(patch: Partial<WeighingQuestionRow>) => patchStage(stage, (s) => ({ ...s, questions: s.questions.map((x) => (x.id === q.id ? { ...x, ...patch } : x)) }))}
                      onOptionRenamed={(from: string, to: string) =>
                        patchStage(stage, (s) => ({
                          ...s,
                          questions: s.questions.map((x) => (x.onlyIfQuestion === q.key && x.onlyIfValue === from ? { ...x, onlyIfValue: to } : x)),
                        }))
                      }
                      onMove={(dir: -1 | 1) =>
                        patchStage(stage, (s) => {
                          const idx = s.questions.findIndex((x) => x.id === q.id);
                          const j = idx + dir;
                          if (idx < 0 || j < 0 || j >= s.questions.length) return s;
                          const next = [...s.questions];
                          [next[idx], next[j]] = [next[j], next[idx]];
                          return { ...s, questions: next };
                        })
                      }
                      onRemove={() => patchStage(stage, (s) => ({ ...s, questions: s.questions.filter((x) => x.id !== q.id) }))}
                    />
                  ))}
                  <button type="button" className="btn sm ghost" onClick={() => patchStage(stage, (s) => ({ ...s, questions: [...s.questions, blankQuestion()] }))}>
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
            <Lock className="ic" style={{ width: 12 }} /> {copy(pc, "fsop.locked")}
          </p>
          <p className="muted small" style={{ margin: "6px 0 0" }}>
            {copy(pc, "fsop.summary.legacy_key_note")}
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
            <span className="muted small">{copy(pc, "fsop.footer.ready")}</span>
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

export function SlotCard({
  pc, index, count, slot, proofKinds, takenKeys, onChange, onMove, onRemove,
}: {
  pc: AdminUiPageContract;
  index: number;
  count: number;
  slot: RemovalProofRow;
  proofKinds: { key: string; label: string; title?: string }[];
  takenKeys: Set<string>;
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
        <button type="button" className="ia del" aria-label={copy(pc, "fsop.proof.remove")} onClick={onRemove}>
          <X className="ic" />
        </button>
      </div>
      <div className="qbody">
        <label className="numfield">
          <span className="numlbl">{copy(pc, "fsop.proof.title")}</span>
          <input
            className="qtext"
            value={slot.title}
            onChange={(e) => {
              const title = e.target.value;
              // A NEW slot's key follows its title until saved; an existing key is never rewritten
              // (it is what the phones stamp on uploads).
              onChange({ title, key: slot.key || slugKey(title, takenKeys, "capture") });
            }}
          />
        </label>
        <label className="numfield">
          <span className="numlbl">{copy(pc, "fsop.proof.hint")}</span>
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
