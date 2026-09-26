"use client";
import Box from "@mui/material/Box";

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
import { useRouter } from "next/navigation";
import { AlertTriangle, Check, ChevronDown, ChevronUp, Lock, Plus, X } from "lucide-react";
import IconButton from "@mui/material/IconButton";
import MuiTextField from "@mui/material/TextField";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import { EditorHeader, InlineSelect, StickyActions, StudioViewToggle, inspectionEditorSx } from "./editor-chrome";
import { blankProofSlot, blankQuestion, keyForTitle, type RemovalProofKind, type RemovalProofRow, type WeighingQuestionRow } from "./weighing-model";
import { QuestionCard } from "./weighing-editor";
import { FEED_STAGES_BY_CODE, emitFeed, feedProblems, type FeedRows, type FeedStage, type FeedStageRows } from "./feed-model";
import { publishedHref } from "./published-href";
import { FeedFlow, type FeedInsert, type FeedRef } from "./feed-flow";
import { publishFeedVersion, saveFeedVersion, type FeedSaveResult } from "./sop-actions";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";
import Alert from "@mui/material/Alert";

type Props = {
  pageContract: AdminUiPageContract;
  basePath: string;
  sopId: string;
  sopName: string;
  sopCode: string;
  versionLabel: string;
  initial: FeedRows;
  /** The view the page opened on (`?view=flow`), read on the server so SSR and client agree. */
  initialView?: "list" | "flow";
};

export function FeedEditor({ pageContract: pc, basePath, sopId, sopName, versionLabel, initial, initialView = "list" }: Props) {
  const router = useRouter();
  const [rows, setRows] = useState<FeedRows>(initial);
  // Keys the loaded version already carries never move; a new capture / question follows its title.
  const [savedKeys] = useState<Set<string>>(
    () => new Set(Object.values(initial.stages).flatMap((s) => (s ? [...s.proofs.map((p) => p.key), ...s.questions.map((q) => q.key)] : [])).filter(Boolean)),
  );
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

  // LIST (default) / FLOW (the chart of the cards). Both views render the same cards below.
  const [view, setView] = useState<"list" | "flow">(initialView);
  const [selectedRef, setSelectedRef] = useState<FeedRef | null>(null);
  const proofKindLabels = useMemo(() => Object.fromEntries(proofKinds.map((k) => [k.key, k.label])), [proofKinds]);
  function switchView(next: "list" | "flow") {
    setView(next);
    if (typeof window !== "undefined") {
      const url = new URL(window.location.href);
      if (next === "flow") url.searchParams.set("view", "flow");
      else url.searchParams.delete("view");
      window.history.replaceState(window.history.state, "", url.toString());
    }
  }
  function insertAt(insert: FeedInsert) {
    if (insert.kind === "proof") {
      const row = blankProofSlot();
      patchStage(insert.stage, (st) => {
        const proofs = [...st.proofs];
        proofs.splice(Math.min(Math.max(insert.index, 0), proofs.length), 0, row);
        return { ...st, proofs };
      });
      setSelectedRef({ stage: insert.stage, kind: "proof", id: row.id });
      return;
    }
    const row = blankQuestion();
    patchStage(insert.stage, (st) => {
      const questions = [...st.questions];
      questions.splice(Math.min(Math.max(insert.index, 0), questions.length), 0, row);
      return { ...st, questions };
    });
    setSelectedRef({ stage: insert.stage, kind: "question", id: row.id });
  }
  function slotCardFor(stage: FeedStage, p: RemovalProofRow, i: number) {
    const block = rows.stages[stage]!;
    return (
      <SlotCard
        kindLabel={copy(pc, "fsop.proofs")}
        key={p.id}
        pc={pc}
        index={i}
        count={block.proofs.length}
        slot={p}
        proofKinds={proofKinds}
        takenKeys={new Set(block.proofs.map((x) => x.key))}
        savedKeys={savedKeys}
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
        onRemove={() => {
          patchStage(stage, (s) => ({ ...s, proofs: s.proofs.filter((x) => x.id !== p.id) }));
          setSelectedRef(null);
        }}
      />
    );
  }
  function questionCardFor(stage: FeedStage, q: WeighingQuestionRow, qi: number) {
    const block = rows.stages[stage]!;
    return (
      <QuestionCard
        key={q.id}
        pc={pc}
        index={qi}
        count={block.questions.length}
        q={q}
        kinds={kinds}
        earlier={block.questions.slice(0, qi)}
        takenKeys={new Set(block.questions.map((x) => x.key))}
        savedKeys={savedKeys}
        onChange={(patch: Partial<WeighingQuestionRow>) =>
          patchStage(stage, (s) => ({
            ...s,
            // A following key that moves takes its dependents' only_if with it.
            questions: s.questions.map((x) =>
              x.id === q.id
                ? { ...x, ...patch }
                : patch.key !== undefined && patch.key !== q.key && q.key && x.onlyIfQuestion === q.key
                  ? { ...x, onlyIfQuestion: patch.key }
                  : x,
            ),
          }))
        }
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
        onRemove={() => {
          patchStage(stage, (s) => ({ ...s, questions: s.questions.filter((x) => x.id !== q.id) }));
          setSelectedRef(null);
        }}
      />
    );
  }
  function cardFor(ref: FeedRef) {
    const block = rows.stages[ref.stage];
    if (!block) return null;
    if (ref.kind === "proof") {
      const i = block.proofs.findIndex((p) => p.id === ref.id);
      return i < 0 ? null : <div className="qlist">{slotCardFor(ref.stage, block.proofs[i], i)}</div>;
    }
    const qi = block.questions.findIndex((q) => q.id === ref.id);
    return qi < 0 ? null : <div className="qlist">{questionCardFor(ref.stage, block.questions[qi], qi)}</div>;
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
    <Box className="kit-enter screen on sop-kit sop-inspection sop-weighing sop-feed" sx={inspectionEditorSx}>
      <EditorHeader
        crumbs={[copy(pc, "crumb"), pc.title, sopName]}
        title={copy(pc, "fsop.title")}
        subtitle={copy(pc, "fsop.subtitle")}
        version={versionLabel}
        notice={copy(pc, "fsop.notice.pinned")}
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
        <Alert severity={result.ok ? "success" : "error"} role="status">
          {result.ok ? <Check className="ic" /> : <AlertTriangle className="ic" />}
          <div>{result.message}</div>
        </Alert>
      ) : null}

      {view === "flow" ? (
        <FeedFlow pc={pc} rows={rows} stages={stages} proofKindLabels={proofKindLabels} selected={selectedRef} onSelect={setSelectedRef} onInsert={insertAt} renderCard={cardFor} />
      ) : null}
      {view === "flow" ? null : stages.map((stage, si) => {
        const block = rows.stages[stage];
        if (!block) return null;
        return (
          <div key={stage}>
          <Card className="card inspection-page">
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
                  {block.proofs.map((p, i) => slotCardFor(stage, p, i))}
                  <Button color="primary" variant="text" size="small" startIcon={<Plus size={14} />} onClick={() => patchStage(stage, (s) => ({ ...s, proofs: [...s.proofs, blankProofSlot()] }))}>
                    {copy(pc, "fsop.proof.add")}
                  </Button>
                </div>
              </div>

              <div className="qcfg" style={{ marginTop: 10 }}>
                <div className="qcfg-head">
                  <span className="qcfg-title">{copy(pc, "fsop.questions")}</span>
                  <span className="muted small">{copy(pc, "fsop.questions.subtitle")}</span>
                </div>
                <div className="qlist">
                  {block.questions.length === 0 ? <p className="muted">{copy(pc, "fsop.questions.empty")}</p> : null}
                  {block.questions.map((q, qi) => questionCardFor(stage, q, qi))}
                  <Button color="primary" variant="text" size="small" startIcon={<Plus size={14} />} onClick={() => patchStage(stage, (s) => ({ ...s, questions: [...s.questions, blankQuestion()] }))}>
                    {copy(pc, "inspection.question.add")}
                  </Button>
                </div>
              </div>
            </div>
          </Card>
          </div>
        );
      })}

      <div>
      <Card className="card inspection-page">
        <div className="bd">
          <p className="muted small" style={{ margin: 0 }}>
            <Lock className="ic" style={{ width: 12 }} /> {copy(pc, "fsop.locked")}
          </p>
          <p className="muted small" style={{ margin: "6px 0 0" }}>
            {copy(pc, "fsop.summary.legacy_key_note")}
          </p>
        </div>
      </Card>
      </div>

      <StickyActions>
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

export function SlotCard({
  pc, kindLabel, index, count, slot, proofKinds, takenKeys, savedKeys, onChange, onMove, onRemove,
}: {
  pc: AdminUiPageContract;
  /** Label of the capture-kind select, from the RENDERING page's own contract (the pages that share
   *  this card carry different copy namespaces). */
  kindLabel: string;
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
          <InlineSelect
            label={kindLabel}
            value={slot.kind}
            minWidth={168}
            options={proofKinds.map((k) => ({ value: k.key, label: k.label }))}
            onChange={(next) => onChange({ kind: next as RemovalProofKind })}
          />
        </span>
        {slot.key ? <code className="muted small">{slot.key}</code> : null}
        <span className="sp" style={{ flex: 1 }} />
        <IconButton type="button" size="small" className="ia" aria-label={copy(pc, "inspection.question.move_up")} disabled={index === 0} onClick={() => onMove(-1)}>
          <ChevronUp className="ic" />
        </IconButton>
        <IconButton type="button" size="small" className="ia" aria-label={copy(pc, "inspection.question.move_down")} disabled={index === count - 1} onClick={() => onMove(1)}>
          <ChevronDown className="ic" />
        </IconButton>
        <IconButton type="button" size="small" className="ia del" aria-label={copy(pc, "fsop.proof.remove")} onClick={onRemove}>
          <X className="ic" />
        </IconButton>
      </div>
      <div className="qbody">
        <label className="numfield">
          <span className="numlbl">{copy(pc, "fsop.proof.title")}</span>
          <MuiTextField
            fullWidth
            size="small"
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
          <span className="numlbl">{copy(pc, "fsop.proof.hint")}</span>
          <MuiTextField fullWidth size="small" value={slot.hint} onChange={(e) => onChange({ hint: e.target.value })} />
        </label>
        {!slot.key ? (
          <label className="numfield">
            <span className="numlbl">{copy(pc, "inspection.question.key")}</span>
            <MuiTextField fullWidth size="small" value={slot.key} onChange={(e) => onChange({ key: e.target.value })} />
          </label>
        ) : null}
        <div className="qfoot">
          <FormControlLabel className="chkline" control={<Checkbox checked={slot.required} onChange={(e) => onChange({ required: e.target.checked })} sx={{ p: { xs: 1.5, sm: 1 } }} />} label={<>{copy(pc, "inspection.question.required")}</>} />
        </div>
      </div>
    </div>
  );
}
