"use client";

// HERD OPERATIONS CAPTURE CARD (maintainer decision 4, 2026-09-16).
//
// The Add birth / Add death form's SOP extras: the instruction shown above the extras, the CAPTURES
// the operator records (live-camera video, photo or either; compulsory or optional) and the
// QUESTIONS they answer, authored beside the form's fixed fields. Every word on this screen is the
// backend contract's; the backend validates the card on save (countssop) and refuses one the phone
// could not render, naming the field. The operator steps after the event (follow_up) are untouched
// by this editor and ride along verbatim.
import { useMemo, useState, useTransition } from "react";
import Link from "@/components/no-prefetch-link";
import { useRouter } from "next/navigation";
import { AlertTriangle, Check, ChevronLeft, Plus } from "lucide-react";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { blankProofSlot, blankQuestion, followQuestionKey, type WeighingQuestionRow } from "./weighing-model";
import { QuestionCard } from "./weighing-editor";
import { SlotCard } from "./feed-editor";
import { captureProblems, emitCaptureCard, type CaptureRows } from "./capture-model";
import { publishedHref } from "./published-href";
import { publishCaptureCardVersion, saveCaptureCardVersion, type FeedSaveResult } from "./sop-actions";

type Props = {
  pageContract: AdminUiPageContract;
  basePath: string;
  sopId: string;
  sopName: string;
  sopCode: string;
  versionLabel: string;
  initial: CaptureRows;
};

export function CaptureCardEditor({ pageContract: pc, basePath, sopId, sopName, sopCode, versionLabel, initial }: Props) {
  const router = useRouter();
  const [rows, setRows] = useState<CaptureRows>(initial);
  // Keys the loaded version already carries never move; a new capture / question follows its title.
  const [savedKeys] = useState<Set<string>>(() => new Set([...initial.proofs, ...initial.questions].map((x) => x.key).filter(Boolean)));
  const [result, setResult] = useState<FeedSaveResult | null>(null);
  const [pending, startTransition] = useTransition();
  const kinds = optionGroup(pc, "wsop_question_kinds");
  const proofKinds = optionGroup(pc, "wsop_proof_kinds");
  const problems = useMemo(() => captureProblems(rows, copy(pc, "capture.proofs"), copy(pc, "capture.question")), [rows, pc]);
  const takenKeys = new Set(rows.questions.map((q) => q.key));
  const form = sopCode === "counts.death" ? copy(pc, "capture.form.death") : copy(pc, "capture.form.birth");

  function submit(publish: boolean) {
    const doc = emitCaptureCard(rows);
    startTransition(async () => {
      const res = publish ? await publishCaptureCardVersion(sopId, doc) : await saveCaptureCardVersion(sopId, doc);
      setResult(res);
      if (res.ok && publish) {
        router.push(publishedHref(basePath, sopId, res.versionNumber));
        router.refresh();
      }
    });
  }

  function move<T extends { id: string }>(list: T[], id: string, dir: -1 | 1): T[] {
    const idx = list.findIndex((x) => x.id === id);
    const j = idx + dir;
    if (idx < 0 || j < 0 || j >= list.length) return list;
    const next = [...list];
    [next[idx], next[j]] = [next[j], next[idx]];
    return next;
  }

  return (
    <div className="screen on sop-inspection sop-weighing sop-capture">
      <div className="phead">
        <div>
          <div className="crumb">
            {copy(pc, "crumb")} · {pc.title} · <b>{sopName}</b>
          </div>
          <h1>{copy(pc, "capture.title")}</h1>
          <div className="sub">
            {form} — {copy(pc, "capture.subtitle")}
          </div>
          <div className="muted small" style={{ marginTop: 4 }}>
            <code>{sopCode}</code> · {versionLabel} · {copy(pc, "capture.notice.pinned")}
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

      <section className="card inspection-page">
        <div className="bd">
          <div className="qcfg">
            <label className="numfield">
              <span className="numlbl">{copy(pc, "capture.instruction")}</span>
              <textarea className="qhelp" rows={3} value={rows.instruction} onChange={(e) => setRows((r) => ({ ...r, instruction: e.target.value }))} />
            </label>
          </div>

          <div className="qcfg" style={{ marginTop: 10 }}>
            <div className="qcfg-head">
              <span className="qcfg-title">{copy(pc, "capture.proofs")}</span>
              <span className="muted small">{copy(pc, "capture.proofs.subtitle")}</span>
            </div>
            <div className="qlist">
              {rows.proofs.length === 0 ? <p className="muted">{copy(pc, "capture.proofs.empty")}</p> : null}
              {rows.proofs.map((p, i) => (
                <SlotCard
                  key={p.id}
                  pc={pc}
                  index={i}
                  count={rows.proofs.length}
                  slot={p}
                  proofKinds={proofKinds}
                  takenKeys={new Set(rows.proofs.map((x) => x.key))}
                  savedKeys={savedKeys}
                  onChange={(patch) => setRows((r) => ({ ...r, proofs: r.proofs.map((x) => (x.id === p.id ? { ...x, ...patch } : x)) }))}
                  onMove={(dir) => setRows((r) => ({ ...r, proofs: move(r.proofs, p.id, dir) }))}
                  onRemove={() => setRows((r) => ({ ...r, proofs: r.proofs.filter((x) => x.id !== p.id) }))}
                />
              ))}
              <button type="button" className="btn sm ghost" onClick={() => setRows((r) => ({ ...r, proofs: [...r.proofs, { ...blankProofSlot(), kind: "photo" }] }))}>
                <Plus className="ic" /> {copy(pc, "capture.proof.add")}
              </button>
            </div>
          </div>

          <div className="qcfg" style={{ marginTop: 10 }}>
            <div className="qcfg-head">
              <span className="qcfg-title">{copy(pc, "capture.questions")}</span>
              <span className="muted small">{copy(pc, "capture.questions.subtitle")}</span>
            </div>
            <div className="qlist">
              {rows.questions.length === 0 ? <p className="muted">{copy(pc, "capture.questions.empty")}</p> : null}
              {rows.questions.map((q, qi) => (
                <QuestionCard
                  key={q.id}
                  pc={pc}
                  index={qi}
                  count={rows.questions.length}
                  q={q}
                  kinds={kinds}
                  earlier={rows.questions.slice(0, qi)}
                  takenKeys={takenKeys}
                  savedKeys={savedKeys}
                  onChange={(patch: Partial<WeighingQuestionRow>) => setRows((r) => ({ ...r, questions: followQuestionKey(r.questions, q.id, patch) }))}
                  onOptionRenamed={(from: string, to: string) =>
                    setRows((r) => ({ ...r, questions: r.questions.map((x) => (x.onlyIfQuestion === q.key && x.onlyIfValue === from ? { ...x, onlyIfValue: to } : x)) }))
                  }
                  onMove={(dir: -1 | 1) => setRows((r) => ({ ...r, questions: move(r.questions, q.id, dir) }))}
                  onRemove={() => setRows((r) => ({ ...r, questions: r.questions.filter((x) => x.id !== q.id) }))}
                />
              ))}
              <button type="button" className="btn sm ghost" onClick={() => setRows((r) => ({ ...r, questions: [...r.questions, blankQuestion()] }))}>
                <Plus className="ic" /> {copy(pc, "inspection.question.add")}
              </button>
            </div>
          </div>
          <p className="muted small" style={{ margin: "10px 0 0" }}>
            {copy(pc, "capture.note.older_app")}
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
            <span className="muted small">{copy(pc, "capture.footer.ready")}</span>
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
