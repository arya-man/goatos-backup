"use client";
import Box from "@mui/material/Box";
import Stack from "@mui/material/Stack";

// SHIFTING SOP (maintainer decision 2026-09-16, docs/decisions/shifting-sop.md).
//
// The shifting cards editor: the RAISE extras (questions and optional captures on the raise form,
// seen by the park head before approval), the COMPLETION card (the captures and questions every
// completion carries) and the HIGH-PRIORITY card (added to a high movement's completion). Every
// word on this screen is the backend contract's; the backend validates the document on save and
// refuses one the phone could not render, naming the field. Park head approval before completion,
// the Feed Config fingerprint on a high-priority movement, the atomic herd move and verifier review
// are maintainer locks and are shown, not edited.
import MenuItem from "@mui/material/MenuItem";
import { useMemo, useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import MuiButton from "@mui/material/Button";
import MuiTextField from "@mui/material/TextField";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { blankProofSlot, blankQuestion, followQuestionKey, keyForTitle, type RemovalProofKind, type RemovalProofRow, type WeighingQuestionRow } from "./weighing-model";
import { QuestionCard } from "./weighing-editor";
import { SHIFTING_SECTIONS, emitShifting, shiftingProblems, type ShiftingRows, type ShiftingSection, type ShiftingSectionRows } from "./shifting-model";
import { publishedHref } from "./published-href";
import { publishShiftingVersion, saveShiftingVersion, type ShiftingSaveResult } from "./sop-actions";
import Alert from "@mui/material/Alert";
import { Iconify } from "@/components/minimal/iconify";
import { EditorHeader } from "./editor-chrome";
import { AddButton, CheckLine, ConfigBox, EDITOR_ICON, EditorCard, EditorPage, GroupTitle, Hint, LockedNote, MoveActions, NumBadge, ProblemList, QuestionShell, Spacer, StickyBar } from "./editor-parts";

type Props = {
  pageContract: AdminUiPageContract;
  basePath: string;
  sopId: string;
  sopName: string;
  sopCode: string;
  versionLabel: string;
  initial: ShiftingRows;
};

export function ShiftingEditor({ pageContract: pc, basePath, sopId, sopName, versionLabel, initial }: Props) {
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
    <EditorPage>
      <EditorHeader
        crumbs={[copy(pc, "crumb"), pc.title, sopName]}
        title={copy(pc, "ssop.title")}
        subtitle={copy(pc, "ssop.subtitle")}
        version={versionLabel}
        notice={copy(pc, "ssop.notice.pinned")}
        backHref={basePath}
      />

      {result ? (
        <Alert severity={result.ok ? "success" : "error"} role="status">
          {result.message}
        </Alert>
      ) : null}

      {SHIFTING_SECTIONS.map((section, si) => {
        const block = rows[section];
        return (
          <EditorCard key={section} badge={si + 1} title={sectionLabel(section)} meta={copy(pc, `ssop.section.${section}.sub`)}>
            <ConfigBox>
              <MuiTextField label={copy(pc, "ssop.instruction")} fullWidth multiline minRows={3} value={block.instruction} onChange={(e) => patchSection(section, (s) => ({ ...s, instruction: e.target.value }))} />
            </ConfigBox>

            <ConfigBox title={<GroupTitle title={copy(pc, "ssop.proofs")} hint={copy(pc, "ssop.proofs.subtitle")} />}>
              <Stack spacing={1.5}>
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
                  <AddButton label={copy(pc, "ssop.proof.add")} onClick={() => patchSection(section, (s) => ({ ...s, proofs: [...s.proofs, blankProofSlot()] }))} />
              </Stack>
            </ConfigBox>

            <ConfigBox title={<GroupTitle title={copy(pc, "ssop.questions")} hint={copy(pc, "ssop.questions.subtitle")} />}>
              <Stack spacing={1.5}>
                  {block.questions.length === 0 ? <Hint>{copy(pc, "ssop.questions.empty")}</Hint> : null}
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
                  <AddButton label={copy(pc, "inspection.question.add")} onClick={() => patchSection(section, (s) => ({ ...s, questions: [...s.questions, blankQuestion()] }))} />
              </Stack>
            </ConfigBox>
          </EditorCard>
        );
      })}

      <EditorCard title={<LockedNote>{copy(pc, "ssop.locked")}</LockedNote>} meta={copy(pc, "ssop.summary.legacy_key_note")} />

      <StickyBar>
        <Box sx={{ minWidth: 0 }}>
          {problems.length ? <ProblemList items={problems} muted /> : <Hint caption>{copy(pc, "ssop.footer.ready")}</Hint>}
        </Box>
        <Spacer />
        <MuiButton type="button" variant="outlined" color="primary" disabled={pending || problems.length > 0} onClick={() => submit(false)}>
          {copy(pc, "inspection.action.save_draft")}
        </MuiButton>
        <MuiButton type="button" variant="contained" color="primary" startIcon={<Iconify icon={EDITOR_ICON.check} />} disabled={pending || problems.length > 0} onClick={() => submit(true)}>
          {copy(pc, "inspection.action.publish")}
        </MuiButton>
      </StickyBar>
    </EditorPage>
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
    <QuestionShell
      head={
        <>
          <NumBadge>{index + 1}</NumBadge>
          <MuiTextField
            select
            label={copy(pc, "ssop.proofs")}
            value={slot.kind}
            onChange={({ target: { value: kind } }) => onChange({ kind: kind as RemovalProofKind })}
            sx={{ minWidth: { xs: 0, sm: 150 }, flexShrink: 0, maxWidth: 1 }}
            slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
          >
            {proofKinds.map((k) => (
              <MenuItem key={k.key} value={k.key}>
                {k.label}
              </MenuItem>
            ))}
          </MuiTextField>
          <Spacer />
          <MoveActions
            upLabel={copy(pc, "inspection.question.move_up")}
            downLabel={copy(pc, "inspection.question.move_down")}
            removeLabel={copy(pc, "ssop.proof.remove")}
            first={index === 0}
            last={index === count - 1}
            onUp={() => onMove(-1)}
            onDown={() => onMove(1)}
            onRemove={onRemove}
          />
        </>
      }
      foot={<CheckLine checked={slot.required} onChange={(required) => onChange({ required })} label={copy(pc, "inspection.question.required")} />}
    >
      <MuiTextField label={copy(pc, "ssop.proof.title")}
        fullWidth
        size="small"
        value={slot.title}
        onChange={(e) => {
          const title = e.target.value;
          // A NEW slot's key follows its title until saved; an existing key is never rewritten
          // (it is what the phones stamp on uploads).
          onChange({ title, key: keyForTitle(title, slot.key, savedKeys, takenKeys, "capture") });
        }}
      />
      <MuiTextField label={copy(pc, "ssop.proof.hint")} fullWidth size="small" value={slot.hint} onChange={(e) => onChange({ hint: e.target.value })} />
      {!slot.key ? (
        <MuiTextField label={copy(pc, "inspection.question.key")} fullWidth size="small" value={slot.key} onChange={(e) => onChange({ key: e.target.value })} />
      ) : null}
    </QuestionShell>
  );
}
