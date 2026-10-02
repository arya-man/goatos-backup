"use client";
import Box from "@mui/material/Box";
import Stack from "@mui/material/Stack";

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
import MenuItem from "@mui/material/MenuItem";
import MuiTextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import Button from "@mui/material/Button";
import { Iconify } from "@/components/minimal/iconify";
import { EditorHeader, InlineSelect, StudioViewToggle } from "./editor-chrome";
import {
  AddButton,
  CheckLine,
  CondRow,
  ConfigBox,
  GroupTitle,
  EDITOR_ICON,
  EditorCard,
  EditorPage,
  FieldGrid,
  Hint,
  IconAction,
  LockedNote,
  MoveActions,
  NumBadge,
  OptionRow,
  ProblemList,
  QuestionShell,
  RadioLine,
  ResultNotice,
  Spacer,
  StickyBar,
} from "./editor-parts";
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
        <Stack spacing={1.5}>
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
        </Stack>
      );
    }
    if (ref.list === "lumpSumProofs") {
      const i = rows.lumpSumProofs.findIndex((p) => p.id === ref.id);
      return i < 0 ? null : <Stack spacing={1.5}>{countedCard(rows.lumpSumProofs[i], i)}</Stack>;
    }
    const list = ref.list as SlotList;
    const i = rows[list].findIndex((p) => p.id === ref.id);
    const labels = list === "removalProofs" ? { title: "wsop.removal.proof.title", hint: "wsop.removal.proof.hint", remove: "wsop.removal.proof.remove" } : { title: "wsop.capture.proof.title", hint: "wsop.capture.proof.hint", remove: "wsop.capture.proof.remove" };
    return i < 0 ? null : <Stack spacing={1.5}>{slotCard(list, rows[list][i], i, labels)}</Stack>;
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
      <Stack spacing={1.5}>
        {qs.length === 0 ? <Hint>{copy(pc, emptyKey)}</Hint> : null}
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
        <AddButton label={copy(pc, "inspection.question.add")} onClick={() => addQuestion(list)} />
      </Stack>
    );
  }

  // One capture slot card (removal card and per-animal share it): kind, title, instruction,
  // key while blank, compulsory.
  function slotCard(list: SlotList, p: RemovalProofRow, i: number, labels: { title: string; hint: string; remove: string }) {
    const slots = rows[list];
    return (
      <QuestionShell
        key={p.id}
        head={
          <>
            <NumBadge>{i + 1}</NumBadge>
            <InlineSelect
              // The capture's KIND ("Video" / "Photo" / "Photo or video"), on every slot card. It
              // borrowed the removal section's heading, so the per-animal cards read "Captures each
              // pen owes".
              label={copy(pc, "wsop.proof.kind.label")}
              value={p.kind}
              minWidth={168}
              options={proofKinds.map((k) => ({ value: k.key, label: k.label }))}
              onChange={(next) => updateSlot(list, p.id, { kind: next as RemovalProofKind })}
            />
            <Spacer />
            <MoveActions
              upLabel={copy(pc, "inspection.question.move_up")}
              downLabel={copy(pc, "inspection.question.move_down")}
              removeLabel={copy(pc, labels.remove)}
              first={i === 0}
              last={i === slots.length - 1}
              onUp={() => moveSlot(list, p.id, -1)}
              onDown={() => moveSlot(list, p.id, 1)}
              onRemove={() => removeSlot(list, p.id)}
            />
          </>
        }
        foot={<CheckLine checked={p.required} onChange={(required) => updateSlot(list, p.id, { required })} label={copy(pc, "inspection.question.required")} />}
      >
        <MuiTextField label={copy(pc, labels.title)}
          fullWidth
          size="small"
          value={p.title}
          onChange={(e) => {
            const title = e.target.value;
            updateSlot(list, p.id, { title, key: keyForTitle(title, p.key, savedKeys, new Set(slots.map((x) => x.key)), "capture") });
          }}
        />
        <MuiTextField label={copy(pc, labels.hint)} fullWidth size="small" value={p.hint} onChange={(e) => updateSlot(list, p.id, { hint: e.target.value })} />
        {!p.key ? (
          <MuiTextField label={copy(pc, "inspection.question.key")} fullWidth size="small" value={p.key} onChange={(e) => updateSlot(list, p.id, { key: e.target.value })} />
        ) : null}
      </QuestionShell>
    );
  }

  // A whole-pen slot card: the same identity, then a COUNT -- how many the operator must record
  // at least and may record at most -- instead of a compulsory tick.
  function countedCard(p: CountedProofRow, i: number) {
    return (
      <QuestionShell
        key={p.id}
        head={
          <>
            <NumBadge>{i + 1}</NumBadge>
            <InlineSelect
              // The capture's KIND, the same "Capture type" every other slot card's select carries; it
              // borrowed the title field's label and read "What the operator capt…" (PR #294 W5).
              label={copy(pc, "wsop.proof.kind.label")}
              value={p.kind}
              minWidth={168}
              options={proofKinds.map((k) => ({ value: k.key, label: k.label }))}
              onChange={(next) => updateCounted(p.id, { kind: next as RemovalProofKind })}
            />
            <Spacer />
            <MoveActions
              upLabel={copy(pc, "inspection.question.move_up")}
              downLabel={copy(pc, "inspection.question.move_down")}
              removeLabel={copy(pc, "wsop.capture.proof.remove")}
              first={i === 0}
              last={i === rows.lumpSumProofs.length - 1}
              onUp={() => moveCounted(p.id, -1)}
              onDown={() => moveCounted(p.id, 1)}
              onRemove={() => removeCounted(p.id)}
            />
          </>
        }
      >
        <MuiTextField label={copy(pc, "wsop.capture.proof.title")}
          fullWidth
          size="small"
          value={p.title}
          onChange={(e) => {
            const title = e.target.value;
            updateCounted(p.id, { title, key: keyForTitle(title, p.key, savedKeys, new Set(rows.lumpSumProofs.map((x) => x.key)), "capture") });
          }}
        />
        <MuiTextField label={copy(pc, "wsop.capture.proof.hint")} fullWidth size="small" value={p.hint} onChange={(e) => updateCounted(p.id, { hint: e.target.value })} />
        {!p.key ? (
          <MuiTextField label={copy(pc, "inspection.question.key")} fullWidth size="small" value={p.key} onChange={(e) => updateCounted(p.id, { key: e.target.value })} />
        ) : null}
        <FieldGrid>
          <MuiTextField label={copy(pc, "wsop.capture.lump_sum.slot_min")} size="small" slotProps={{ htmlInput: { inputMode: "numeric" } }} value={p.min} onChange={(e) => updateCounted(p.id, { min: e.target.value })} />
          <MuiTextField label={`${copy(pc, "wsop.capture.lump_sum.slot_max")} (≤ ${LUMP_SUM_VIDEO_CEILING})`} size="small" slotProps={{ htmlInput: { inputMode: "numeric" } }} value={p.max} onChange={(e) => updateCounted(p.id, { max: e.target.value })} />
        </FieldGrid>
      </QuestionShell>
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
    <EditorPage>
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

      <ResultNotice result={result} okLabel={copy(pc, "modal.builder.notice_ok")} />

      {view === "flow" ? (
        <WeighingFlow pc={pc} rows={rows} proofKindLabels={proofKindLabels} selected={selectedRef} onSelect={setSelectedRef} onInsert={insertAt} renderCard={cardFor} />
      ) : null}
      {view === "flow" ? null : (
      <>
      {/* 1. Planning */}
      <EditorCard badge={1} title={copy(pc, "wsop.section.planning")} meta={copy(pc, "wsop.section.planning.subtitle")}>
        <ConfigBox title={copy(pc, "wsop.planning.modes")}>
          <Stack>
            {WEIGHING_MODES.map((mode) => (
              <CheckLine key={mode} checked={rows.modes.includes(mode)} onChange={(on) => toggleMode(mode, on)} label={copy(pc, `wsop.planning.mode.${mode}`)} />
            ))}
          </Stack>
          <FieldGrid>
            <MuiTextField label={copy(pc, "wsop.planning.default_cap")} size="small" slotProps={{ htmlInput: { inputMode: "numeric" } }} value={rows.defaultCapPerDay} onChange={(e) => setRows((r) => ({ ...r, defaultCapPerDay: e.target.value }))} />
          </FieldGrid>
        </ConfigBox>
      </EditorCard>

      {/* 2. Feed & water removal */}
      <EditorCard badge={2} title={copy(pc, "wsop.section.removal")} meta={copy(pc, "wsop.section.removal.subtitle")}>
        <ConfigBox title={copy(pc, "wsop.removal.mode")}>
          <Stack>
            {removalModes.map((m) => (
              <RadioLine
                key={m.key}
                name="wsop-removal-mode"
                value={m.key}
                checked={rows.removalMode === m.key}
                onChange={() => setRows((r) => ({ ...r, removalMode: m.key as RemovalMode }))}
                label={
                  <>
                    <Box component="strong" sx={{ fontWeight: "fontWeightSemiBold" }}>{m.label}</Box>
                    {m.title ? <Box component="span" sx={{ color: "text.secondary" }}> — {m.title}</Box> : null}
                  </>
                }
              />
            ))}
          </Stack>
        </ConfigBox>
        {removalOn ? (
          <>
            <ConfigBox>
              <MuiTextField label={copy(pc, "wsop.removal.instruction")} fullWidth multiline minRows={3} value={rows.removalInstruction} onChange={(e) => setRows((r) => ({ ...r, removalInstruction: e.target.value }))} />
              <FieldGrid>
                <CutoffTimeField
                  // Remounts when the draft is replaced from outside (another version loaded).
                  key={rows.removalCutoffTime}
                  label={copy(pc, "wsop.removal.cutoff")}
                  hourLabel={copy(pc, "wsop.removal.cutoff.hour")}
                  minuteLabel={copy(pc, "wsop.removal.cutoff.minute")}
                  hint={copy(pc, "wsop.removal.cutoff.hint")}
                  value={rows.removalCutoffTime}
                  onChange={(next) => setRows((r) => ({ ...r, removalCutoffTime: next }))}
                />
              </FieldGrid>
            </ConfigBox>
            <ConfigBox title={<GroupTitle title={copy(pc, "wsop.removal.proofs")} hint={copy(pc, "wsop.removal.proofs.subtitle")} />}>
              <Stack spacing={1.5}>
                {rows.removalProofs.map((p, i) => slotCard("removalProofs", p, i, { title: "wsop.removal.proof.title", hint: "wsop.removal.proof.hint", remove: "wsop.removal.proof.remove" }))}
                <AddButton label={copy(pc, "wsop.removal.proof.add")} onClick={() => addSlot("removalProofs")} />
              </Stack>
            </ConfigBox>
            <ConfigBox title={<GroupTitle title={copy(pc, "wsop.removal.questions")} hint={copy(pc, "wsop.removal.questions.subtitle")} />}>
              {questionList("removalQuestions", "wsop.removal.questions.empty")}
            </ConfigBox>
          </>
        ) : (
          <Hint>{copy(pc, "wsop.removal.off_note")}</Hint>
        )}
      </EditorCard>

      {/* 3. Capture */}
      <EditorCard badge={3} title={copy(pc, "wsop.section.capture")} meta={copy(pc, "wsop.section.capture.subtitle")}>
        {/* 3a. Per animal */}
        <ConfigBox title={<GroupTitle title={copy(pc, "wsop.capture.individual.title")} hint={copy(pc, "wsop.capture.individual.subtitle")} />}>
          <CheckLine
            disabled
            checked={rows.individualVideoRequired}
            onChange={() => undefined}
            label={
              <>
                {copy(pc, "wsop.capture.individual.video")}{" "}
                <LockedNote>{copy(pc, "wsop.capture.individual.locked_short")}</LockedNote>
              </>
            }
          />
          <ConfigBox title={<GroupTitle title={copy(pc, "wsop.capture.individual.proofs")} hint={copy(pc, "wsop.capture.individual.proofs.subtitle")} />}>
            <Stack spacing={1.5}>
              {rows.individualProofs.map((p, i) => slotCard("individualProofs", p, i, { title: "wsop.capture.proof.title", hint: "wsop.capture.proof.hint", remove: "wsop.capture.proof.remove" }))}
              <AddButton label={copy(pc, "wsop.capture.individual.add_capture")} onClick={() => addSlot("individualProofs")} />
            </Stack>
            <Hint caption>{copy(pc, "wsop.capture.individual.at_least_one")}</Hint>
          </ConfigBox>
          <ConfigBox title={<GroupTitle title={copy(pc, "wsop.capture.individual.questions")} hint={copy(pc, "wsop.capture.individual.questions.subtitle")} />}>
            {questionList("individualQuestions", "wsop.capture.individual.questions.empty")}
          </ConfigBox>
        </ConfigBox>

        {/* 3b. Whole pen */}
        <ConfigBox title={<GroupTitle title={copy(pc, "wsop.capture.lump_sum.title")} hint={copy(pc, "wsop.capture.lump_sum.subtitle")} />}>
          <ConfigBox title={<GroupTitle title={copy(pc, "wsop.capture.lump_sum.proofs")} hint={copy(pc, "wsop.capture.lump_sum.proofs.subtitle")} />}>
            <Stack spacing={1.5}>
              {rows.lumpSumProofs.map((p, i) => countedCard(p, i))}
              <AddButton label={copy(pc, "wsop.capture.lump_sum.add_capture")} onClick={addCounted} />
            </Stack>
            <Hint caption>
              {copy(pc, "wsop.capture.lump_sum.total_ceiling")} · {fillCopy(copy(pc, "wsop.summary.lump_sum_videos"), { min: lumpWindow.min, max: lumpWindow.max })}
            </Hint>
          </ConfigBox>
          <ConfigBox title={<GroupTitle title={copy(pc, "wsop.capture.lump_sum.questions")} hint={copy(pc, "wsop.capture.lump_sum.questions.subtitle")} />}>
            {questionList("lumpSumQuestions", "wsop.capture.lump_sum.questions.empty")}
          </ConfigBox>
        </ConfigBox>

        <Hint caption>{copy(pc, "wsop.capture.locked_rules")}</Hint>
      </EditorCard>
      </>
      )}
      <StickyBar>
        <Box sx={{ minWidth: 0 }}>
          {problems.length ? <ProblemList items={problems} muted /> : <Hint caption>{copy(pc, "wsop.footer.ready")}</Hint>}
        </Box>
        <Spacer />
        <Button color="primary" variant="outlined" loading={pending} disabled={pending || problems.length > 0} onClick={() => submit(false)}>
          {copy(pc, "inspection.action.save_draft")}
        </Button>
        <Button variant="contained" color="primary" startIcon={<Iconify icon={EDITOR_ICON.check} />} disabled={pending || problems.length > 0} onClick={() => submit(true)}>
          {copy(pc, "inspection.action.publish")}
        </Button>
      </StickyBar>
    </EditorPage>
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
    <QuestionShell
      head={
        <>
          <NumBadge>{index + 1}</NumBadge>
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
          <Spacer />
          <MoveActions
            upLabel={copy(pc, "inspection.question.move_up")}
            downLabel={copy(pc, "inspection.question.move_down")}
            removeLabel={copy(pc, "inspection.question.remove")}
            first={index === 0}
            last={index === count - 1}
            onUp={() => onMove(-1)}
            onDown={() => onMove(1)}
            onRemove={onRemove}
          />
        </>
      }
      foot={
        <>
          <CheckLine checked={q.required} onChange={(required) => onChange({ required })} label={copy(pc, "inspection.question.required")} />
          <CondRow>
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
              <InlineSelect
                label={copy(pc, "inspection.question.only_if_value")}
                value={q.onlyIfValue}
                minWidth={120}
                options={[{ value: "", label: "—" }, ...dep.options.map((o) => ({ value: o.value, label: o.label }))]}
                onChange={(next) => onChange({ onlyIfValue: next })}
              />
            ) : null}
          </CondRow>
        </>
      }
    >
      <MuiTextField label={copy(pc, "wsop.question.title")}
        fullWidth
        size="small"
        value={q.title}
        onChange={(e) => {
          const title = e.target.value;
          onChange({ title, key: keyForTitle(title, q.key, savedKeys, takenKeys) });
        }}
      />
      <MuiTextField label={copy(pc, "inspection.question.hint")} fullWidth multiline minRows={2} value={q.hint} onChange={(e) => onChange({ hint: e.target.value })} />
      {!q.key ? (
        <MuiTextField label={copy(pc, "inspection.question.key")} fullWidth size="small" value={q.key} onChange={(e) => onChange({ key: e.target.value })} />
      ) : null}

      {q.kind === "choice" || q.kind === "multi" ? (
        <ConfigBox
          title={copy(pc, "inspection.question.options")}
          action={<AddButton label={copy(pc, "inspection.question.add_option")} onClick={() => onChange({ options: [...q.options, { value: "", label: "" }] })} />}
        >
          {q.options.map((o, i) => (
            <OptionRow
              key={i}
              multi={q.kind !== "choice"}
              action={<IconAction icon={EDITOR_ICON.remove} danger label={copy(pc, "wsop.question.remove_choice")} onClick={() => onChange({ options: q.options.filter((_, j) => j !== i) })} />}
            >
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
            </OptionRow>
          ))}
          {q.kind === "choice" ? (
            <CheckLine checked={q.allowOther} onChange={(allowOther) => onChange({ allowOther })} label={copy(pc, "inspection.question.allow_other")} />
          ) : null}
        </ConfigBox>
      ) : null}

      {q.kind === "number" ? (
        <ConfigBox>
          <FieldGrid>
            <MuiTextField label={copy(pc, "inspection.question.min")} size="small" value={q.min} slotProps={{ htmlInput: { inputMode: "decimal" } }} onChange={(e) => onChange({ min: e.target.value })} />
            <MuiTextField label={copy(pc, "inspection.question.max")} size="small" value={q.max} slotProps={{ htmlInput: { inputMode: "decimal" } }} onChange={(e) => onChange({ max: e.target.value })} />
            <MuiTextField label={copy(pc, "inspection.question.unit")} size="small" value={q.unit} onChange={(e) => onChange({ unit: e.target.value })} />
          </FieldGrid>
        </ConfigBox>
      ) : null}
    </QuestionShell>
  );
}

const CUTOFF_HOURS = Array.from({ length: 24 }, (_, i) => String(i).padStart(2, "0"));
const CUTOFF_MINUTES = Array.from({ length: 12 }, (_, i) => String(i * 5).padStart(2, "0"));

/**
 * The removal evening as two listboxes (hour, minute) instead of the native time input, which the
 * console bans: the same `HH:MM` value the model validates, or "" for the farm-wide evening. A
 * half-picked time stays local until both halves are chosen, so the draft never holds "20:".
 */
function CutoffTimeField({
  label,
  hourLabel,
  minuteLabel,
  hint,
  value,
  onChange,
}: {
  label: string;
  hourLabel: string;
  minuteLabel: string;
  hint: string;
  value: string;
  onChange: (next: string) => void;
}) {
  const [h0 = "", m0 = ""] = value.split(":");
  const [hour, setHour] = useState(CUTOFF_HOURS.includes(h0) ? h0 : "");
  const [minute, setMinute] = useState(m0);
  const minutes = minute && !CUTOFF_MINUTES.includes(minute) ? [...CUTOFF_MINUTES, minute].sort() : CUTOFF_MINUTES;
  const commit = (nextHour: string, nextMinute: string) => {
    setHour(nextHour);
    setMinute(nextMinute);
    if (nextHour === "" && nextMinute === "") onChange("");
    else if (nextHour !== "" && nextMinute !== "") onChange(`${nextHour}:${nextMinute}`);
  };
  const selectProps = { inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } };
  return (
    <Box role="group" aria-label={label}>
      <Typography variant="subtitle2" component="p" sx={{ mb: 1 }}>
        {label}
      </Typography>
      <Stack direction="row" spacing={1.25}>
        <MuiTextField select size="small" label={hourLabel} value={hour} onChange={(e) => (e.target.value === "" ? commit("", "") : commit(e.target.value, minute || "00"))} sx={{ minWidth: 96 }} slotProps={selectProps}>
          <MenuItem value="">--</MenuItem>
          {CUTOFF_HOURS.map((h) => (
            <MenuItem key={h} value={h}>
              {h}
            </MenuItem>
          ))}
        </MuiTextField>
        <MuiTextField select size="small" label={minuteLabel} value={minute} onChange={(e) => commit(hour, e.target.value)} sx={{ minWidth: 96 }} slotProps={selectProps}>
          <MenuItem value="">--</MenuItem>
          {minutes.map((m) => (
            <MenuItem key={m} value={m}>
              {m}
            </MenuItem>
          ))}
        </MuiTextField>
      </Stack>
      <Typography variant="caption" component="p" sx={{ mt: 0.75, color: "text.secondary" }}>
        {hint}
      </Typography>
    </Box>
  );
}
