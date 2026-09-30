"use client";

import { useRouter } from "next/navigation";
import { useCallback, useMemo, useRef, useState, useTransition } from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import CardContent from "@mui/material/CardContent";
import CardHeader from "@mui/material/CardHeader";
import Chip from "@mui/material/Chip";
import IconButton from "@mui/material/IconButton";
import ListSubheader from "@mui/material/ListSubheader";
import MenuItem from "@mui/material/MenuItem";
import Stack from "@mui/material/Stack";
import Tab from "@mui/material/Tab";
import Tabs from "@mui/material/Tabs";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";

import { Iconify } from "@/components/minimal/iconify";
import { Label } from "@/components/minimal/label";

import { copy, optionalCopy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { Caption } from "@/components/app/caption";
import type {
  HealthConfigFieldError,
  HealthRegisterDetail,
  HealthRegisterDocument,
  HealthRegisterOption,
  HealthRegisterProblem,
  HealthRegisterQuestion,
  HealthRegisterRule,
} from "@/lib/api/server";

import { mintKey } from "./health-register-keys";
import {
  buildSignIndex,
  derivedSign,
  illnessLabel,
  signChoices,
  signLabel,
  withSignAdded,
  type SignIndex,
} from "./health-register-model";
import {
  discardRegisterDraft,
  publishRegisterDraft,
  saveRegisterDraft,
  type HealthRegisterActionResult,
} from "./health-register-actions";
import Alert from "@mui/material/Alert";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";

// The diagnosis-register editor.
//
// WHAT THE AUTHOR IS ACTUALLY EDITING, and why the screen is shaped this way: a question and the
// rule that reads it are joined by a FINDING — a short token an answer produces and a rule names.
// That join is the whole mechanism, so it is visible on both sides of this screen rather than
// hidden behind a picker: an answer shows the findings it produces, and a rule shows the findings
// it looks for. An author who mistypes one sees two lists that no longer line up, and the backend
// says so in the same words.
//
// TOKENS ARE NEVER NORMALISED HERE. Splitting a comma-separated list and trimming the gaps around
// the commas is parsing; changing "Nasal_Discharge" to "nasal_discharge" would silently connect or
// disconnect a rule from the answer that fires it. Everything is sent exactly as typed, and the
// backend's check is what tells the author it does not match.

type SectionKey = "questions" | "rules";

/**
 * One write and what to do once it lands. Named so the signature below stays readable.
 *
 * Written in call-signature form rather than as an arrow type because `=> Promise<X>` reads to the
 * copy-firewall scan as JSX text -- a `>`, capitalised words, then a `<`. The scan is right to be
 * blunt about that shape; this is simply not prose.
 */
type RegisterWrite = {
  (): Promise<HealthRegisterActionResult>;
};

/**
 * Clears the key after a CONFIRMED success, so the next press of the same button is a
 * new intent and a failed press can still be retried under the key it already used.
 */
function rotate(ref: { current: string }) {
  return (result: HealthRegisterActionResult) => {
    if (result.ok) ref.current = "";
    return result;
  };
}

function splitTokens(raw: string): string[] {
  return raw
    .split(",")
    .map((t) => t.trim())
    .filter((t) => t.length > 0);
}

function joinTokens(tokens: string[] | undefined): string {
  return (tokens ?? []).join(", ");
}

/** Errors whose path starts with this prefix, so a row can mark itself. */
function errorsFor(errors: HealthConfigFieldError[] | undefined, prefix: string): HealthConfigFieldError[] {
  if (!errors) return [];
  return errors.filter((e) => e.field === prefix || e.field.startsWith(`${prefix}.`));
}

/** Secondary caption line (was `.small.muted`). */
function Muted({ children, sx }: { children: React.ReactNode; sx?: object }) {
  return (
    <Typography variant="body2" component="span" sx={{ color: "text.secondary", ...sx }}>
      {children}
    </Typography>
  );
}

/** An in-card empty line (was `.muted.small` with inline padding). */
function EmptyLine({ children }: { children: React.ReactNode }) {
  return (
    <Typography variant="body2" component="div" sx={{ color: "text.secondary", py: 2, px: 0.5, textAlign: "center" }}>
      {children}
    </Typography>
  );
}

/** The template "add a row" action under a list. */
function AddButton({ onClick, children }: { onClick: () => void; children: React.ReactNode }) {
  return (
    <Button
      type="button"
      variant="outlined"
      color="inherit"
      size="small"
      startIcon={<Iconify icon="mingcute:add-line" aria-hidden="true" />}
      onClick={onClick}
      sx={{ alignSelf: "flex-start" }}
    >
      {children}
    </Button>
  );
}

/** Remove one row: a template IconButton with the trash icon, named by its aria-label. */
function RemoveButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <IconButton type="button" aria-label={label} title={label} onClick={onClick} sx={{ color: "error.main" }}>
      <Iconify icon="solar:trash-bin-trash-bold" aria-hidden="true" />
    </IconButton>
  );
}

/** A nested block inside a card (question / illness / answer): divider border, template radius. */
const BLOCK_SX = { border: 1, borderColor: "divider", borderRadius: "var(--r-lg)" } as const;

/** A labelled TextField that keeps the old `.fld` flex basis on its row. */
const fieldSx = (flex: string) => ({ flex, minWidth: 0 });

export function RegisterEditor({
  detail,
  pageContract,
  mayWrite,
  disabledReason,
  listHref,
}: {
  detail: HealthRegisterDetail;
  pageContract: AdminUiPageContract;
  mayWrite: boolean;
  disabledReason: string;
  listHref: string;
}) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [doc, setDoc] = useState<HealthRegisterDocument>(detail.document);
  const [result, setResult] = useState<HealthRegisterActionResult | null>(null);
  const [open, setOpen] = useState<SectionKey>("questions");

  // One key per human INTENT, minted on the first press, reused across retries of that same press,
  // and rotated only after a confirmed success. That is what makes a network-failed save safe to
  // press again: the replay returns the original result instead of writing twice.
  //
  // Each is minted in its HANDLER, never during render. A key generated while rendering changes on
  // every re-render, so the retry would carry a different key and publish twice -- the exact
  // opposite of what the key is for.
  const saveKey = useRef<string>("");
  const publishKey = useRef<string>("");
  const discardKey = useRef<string>("");

  // The lens the whole screen reads: which answer produces which sign, and which illness
  // looks at it. Rebuilt on every edit so the links are never a step behind the document.
  const index = useMemo(() => buildSignIndex(doc), [doc]);

  const isDraft = detail.status === "draft";
  const editable = mayWrite && isDraft;
  const fieldErrors = result && !result.ok ? result.fieldErrors : undefined;

  /**
   * Run one write and then do exactly ONE of two things: stay and re-read, or leave.
   *
   * Refreshing after a push supersedes it -- both are transitions on the router, the
   * refresh re-renders the route the push was leaving, and the navigation never lands.
   * That is why publish wrote a new live register and left the author sitting on the
   * draft they had just published, with nothing on screen saying it had worked.
   */
  const run = useCallback(
    (fn: RegisterWrite, opts?: { navigateTo?: string }) => {
      startTransition(async () => {
        const r = await fn();
        setResult(r);
        if (r.ok && opts?.navigateTo) {
          router.push(opts.navigateTo);
          return;
        }
        router.refresh();
      });
    },
    [router],
  );

  const onSave = () => {
    if (!saveKey.current) saveKey.current = mintKey("register-save");
    const key = saveKey.current;
    // A save STAYS: the author keeps editing, and the refresh brings back the verdict a
    // publish would now apply.
    run(() => saveRegisterDraft(detail.animal_class, doc, key).then(rotate(saveKey)));
  };

  const onPublish = () => {
    if (!publishKey.current) publishKey.current = mintKey("register-publish");
    const key = publishKey.current;
    // A publish LEAVES: this draft no longer exists as a draft, so staying on it would
    // show a version that is now the live register under an editor that cannot save.
    run(() => publishRegisterDraft(detail.register_version_id, key).then(rotate(publishKey)), {
      navigateTo: listHref,
    });
  };

  const onDiscard = () => {
    if (!discardKey.current) discardKey.current = mintKey("register-discard");
    const key = discardKey.current;
    // A discard LEAVES for the same reason, and more bluntly: the version is gone.
    run(() => discardRegisterDraft(detail.register_version_id, key).then(rotate(discardKey)), {
      navigateTo: listHref,
    });
  };

  const patchQuestion = (index: number, patch: Partial<HealthRegisterQuestion>) =>
    setDoc((d) => ({
      ...d,
      questions: d.questions.map((q, i) => (i === index ? { ...q, ...patch } : q)),
    }));

  const patchRule = (index: number, patch: Partial<HealthRegisterRule>) =>
    setDoc((d) => ({ ...d, rules: d.rules.map((r, i) => (i === index ? { ...r, ...patch } : r)) }));

  return (
    <>
      <ResultBand result={result} pageContract={pageContract} />

      {!mayWrite ? (
        <Alert severity="error" sx={{ mb: 2 }}><div>{disabledReason}</div>
        </Alert>
      ) : null}

      <RegisterProblems
        problems={detail.problems ?? undefined}
        pageContract={pageContract}
        heading={copy(pageContract, "label.warnings")}
        note={optionalCopy(pageContract, "note.register_warnings")}
      />

      <Stack
        direction="row"
        spacing={1}
        useFlexGap
        sx={{ alignItems: "center", flexWrap: "wrap", mb: 1.5 }}
      >
        {/* Client-state section switch (template Tabs + Label counts); not a URL tab. */}
        <Tabs value={open} onChange={(_, next: SectionKey) => setOpen(next)} sx={{ minWidth: 0 }}>
          <Tab
            value="questions"
            label={copy(pageContract, "section.questions.title")}
            iconPosition="end"
            icon={<Label variant={open === "questions" ? "filled" : "soft"}>{doc.questions.length}</Label>}
          />
          <Tab
            value="rules"
            label={copy(pageContract, "section.rules.title")}
            iconPosition="end"
            icon={<Label variant={open === "rules" ? "filled" : "soft"}>{doc.rules.length}</Label>}
          />
        </Tabs>
        <Box sx={{ flex: 1 }} />
        <Button type="button" variant="soft" color="primary" onClick={onSave} disabled={!editable || pending}>
          {copy(pageContract, "action.save_draft")}
        </Button>
        <Button type="button" variant="contained" color="primary" onClick={onPublish} disabled={!editable || pending}>
          {copy(pageContract, "action.publish_register")}
        </Button>
        <Button type="button" variant="outlined" color="inherit" onClick={onDiscard} disabled={!editable || pending}>
          {copy(pageContract, "action.discard_register_draft")}
        </Button>
      </Stack>

      {open === "questions" ? (
        <Card component="section" sx={{ mb: 2 }}>
          <CardHeader
            title={copy(pageContract, "section.questions.title")}
            subheader={copy(pageContract, "section.questions.caption")}
          />
          <Caption>{copy(pageContract, "note.questions_how")}</Caption>
          <CardContent>
            <Stack spacing={1.5}>
              {doc.questions.length === 0 ? (
                <EmptyLine>{copy(pageContract, "empty.questions")}</EmptyLine>
              ) : (
                doc.questions.map((q, i) => (
                  <QuestionRow
                    key={`${q.id}-${i}`}
                    question={q}
                    index={i}
                    editable={editable}
                    pageContract={pageContract}
                    signIndex={index}
                    errors={errorsFor(fieldErrors, `questions.${i}`)}
                    onChange={(patch) => patchQuestion(i, patch)}
                    onAddToIllness={(token, ruleId) =>
                      setDoc((d) => ({
                        ...d,
                        rules: d.rules.map((r) =>
                          r.id === ruleId ? withSignAdded(r, "probable", token) : r,
                        ),
                      }))
                    }
                    illnesses={doc.rules.map((r) => r.id).filter(Boolean)}
                    onRemove={() =>
                      setDoc((d) => ({ ...d, questions: d.questions.filter((_, k) => k !== i) }))
                    }
                  />
                ))
              )}
              {editable ? (
                <AddButton
                  onClick={() =>
                    setDoc((d) => ({
                      ...d,
                      questions: [
                        ...d.questions,
                        {
                          id: "",
                          kind: "choice",
                          title: "",
                          options: [
                            { value: "no", label: copy(pageContract, "label.answer_no") },
                            { value: "yes", label: copy(pageContract, "label.answer_yes"), emits: [] },
                          ],
                        },
                      ],
                    }))
                  }
                >
                  {copy(pageContract, "action.add_question")}
                </AddButton>
              ) : null}
            </Stack>
          </CardContent>
        </Card>
      ) : (
        <Card component="section" sx={{ mb: 2 }}>
          <CardHeader
            title={copy(pageContract, "section.rules.title")}
            subheader={copy(pageContract, "section.rules.caption")}
          />
          <Caption>{copy(pageContract, "note.rules_how")}</Caption>
          <CardContent>
            <Stack spacing={1.5}>
              {doc.rules.length === 0 ? (
                <EmptyLine>{copy(pageContract, "empty.rules")}</EmptyLine>
              ) : (
                doc.rules.map((r, i) => (
                  <RuleRow
                    key={`${r.id}-${i}`}
                    rule={r}
                    index={i}
                    editable={editable}
                    pageContract={pageContract}
                    signIndex={index}
                    choices={signChoices(doc)}
                    errors={errorsFor(fieldErrors, `rules.${i}`)}
                    onChange={(patch) => patchRule(i, patch)}
                    onRemove={() => setDoc((d) => ({ ...d, rules: d.rules.filter((_, k) => k !== i) }))}
                  />
                ))
              )}
              {editable ? (
                <AddButton
                  onClick={() =>
                    setDoc((d) => ({
                      ...d,
                      rules: [...d.rules, { id: "", severity_base: 2, pathognomonic: [{ findings: [] }] }],
                    }))
                  }
                >
                  {copy(pageContract, "action.add_rule")}
                </AddButton>
              ) : null}
            </Stack>
          </CardContent>
        </Card>
      )}
    </>
  );
}

function QuestionRow({
  question,
  index,
  editable,
  pageContract,
  signIndex,
  illnesses,
  errors,
  onChange,
  onAddToIllness,
  onRemove,
}: {
  question: HealthRegisterQuestion;
  index: number;
  editable: boolean;
  pageContract: AdminUiPageContract;
  signIndex: SignIndex;
  illnesses: string[];
  errors: HealthConfigFieldError[];
  onChange: (patch: Partial<HealthRegisterQuestion>) => void;
  onAddToIllness: (token: string, ruleId: string) => void;
  onRemove: () => void;
}) {
  const options = question.options ?? [];
  const patchOption = (i: number, patch: Partial<HealthRegisterOption>) =>
    onChange({ options: options.map((o, k) => (k === i ? { ...o, ...patch } : o)) });

  return (
    <Stack spacing={1} sx={{ ...BLOCK_SX, p: 2, borderColor: errors.length ? "error.main" : "divider" }}>
      <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: "wrap", alignItems: "flex-start" }}>
        <TextField
          size="small"
          label={copy(pageContract, "label.question_title")}
          value={question.title}
          disabled={!editable}
          onChange={(e) => onChange({ title: e.target.value })}
          slotProps={{ htmlInput: { "aria-label": `${copy(pageContract, "label.question_title")} ${index + 1}` } }}
          sx={fieldSx("2 1 240px")}
        />
        <TextField
          size="small"
          label={copy(pageContract, "label.question_id")}
          value={question.id}
          disabled={!editable}
          onChange={(e) => onChange({ id: e.target.value })}
          sx={fieldSx("1 1 140px")}
        />
        <TextField
          select
          size="small"
          label={copy(pageContract, "label.question_kind")}
          value={question.kind}
          disabled={!editable}
          onChange={(e) => onChange({ kind: e.target.value as HealthRegisterQuestion["kind"] })}
          sx={fieldSx("1 1 150px")}
        >
          <MenuItem value="choice">{copy(pageContract, "label.kind.choice")}</MenuItem>
          <MenuItem value="multi">{copy(pageContract, "label.kind.multi")}</MenuItem>
          <MenuItem value="number">{copy(pageContract, "label.kind.number")}</MenuItem>
        </TextField>
        {editable ? <RemoveButton label={copy(pageContract, "action.remove_question")} onClick={onRemove} /> : null}
      </Stack>

      {question.kind === "number" ? (
        <BandList question={question} editable={editable} onChange={onChange} pageContract={pageContract} />
      ) : (
        <Stack spacing={0.75}>
          <Muted>{copy(pageContract, "label.answers")}</Muted>
          {options.map((o, i) => (
            <AnswerRow
              key={`${o.value}-${i}`}
              question={question}
              option={o}
              index={i}
              editable={editable}
              pageContract={pageContract}
              signIndex={signIndex}
              illnesses={illnesses}
              onChange={(patch) => patchOption(i, patch)}
              onAddToIllness={onAddToIllness}
              onRemove={() => onChange({ options: options.filter((_, k) => k !== i) })}
            />
          ))}
          {editable ? (
            <AddButton onClick={() => onChange({ options: [...options, { value: "", label: "", emits: [] }] })}>
              {copy(pageContract, "action.add_answer")}
            </AddButton>
          ) : null}
        </Stack>
      )}

      <RowErrors errors={errors} />
    </Stack>
  );
}

/**
 * One answer, and the only place the word "sign" is decided.
 *
 * A layman's whole job on this screen is here: tick whether an answer MEANS SOMETHING IS
 * WRONG, and see which illnesses it points to. The machine token is derived from the
 * question and the answer rather than typed -- nobody has to know it exists -- and is
 * shown small and muted for the reader who does.
 */
function AnswerRow({
  question,
  option,
  index,
  editable,
  pageContract,
  signIndex,
  illnesses,
  onChange,
  onAddToIllness,
  onRemove,
}: {
  question: HealthRegisterQuestion;
  option: HealthRegisterOption;
  index: number;
  editable: boolean;
  pageContract: AdminUiPageContract;
  signIndex: SignIndex;
  illnesses: string[];
  onChange: (patch: Partial<HealthRegisterOption>) => void;
  onAddToIllness: (token: string, ruleId: string) => void;
  onRemove: () => void;
}) {
  const token = (option.emits ?? [])[0] ?? "";
  const isSign = Boolean(token);
  const pointsTo = token ? signIndex.usedBy.get(token) ?? [] : [];
  const unlinked = illnesses.filter((id) => !pointsTo.includes(id));

  return (
    <Stack spacing={0.75} sx={{ ...BLOCK_SX, px: 1.25, py: 1 }}>
      <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: "wrap", alignItems: "center" }}>
        <TextField
          size="small"
          label={copy(pageContract, "label.answer_label")}
          value={option.label}
          disabled={!editable}
          onChange={(e) => onChange({ label: e.target.value })}
          slotProps={{ htmlInput: { "aria-label": `${copy(pageContract, "label.answer_label")} ${index + 1}` } }}
          sx={fieldSx("1 1 180px")}
        />
        <FormControlLabel disabled={!editable} control={<Checkbox checked={isSign} disabled={!editable} onChange={(e) =>
              onChange({
                // Ticking DERIVES the token; unticking drops it. An author never types one,
                // and an answer that means nothing is wrong carries none -- which is what
                // keeps "Eating normally" out of the evidence the engine reasons over.
                emits: e.target.checked ? [derivedSign(question, option.value)] : [],
              })
            } sx={{ p: { xs: 1.5, sm: 1 } }} />} label={<>{copy(pageContract, isSign ? "label.is_a_sign" : "label.not_a_sign")}</>}
          slotProps={{ typography: { variant: "body2" } }} />
        {editable ? (
          <RemoveButton label={`${copy(pageContract, "action.remove_answer")} ${index + 1}`} onClick={onRemove} />
        ) : null}
      </Stack>

      {isSign ? (
        <Stack direction="row" spacing={0.75} useFlexGap sx={{ flexWrap: "wrap", alignItems: "center" }}>
          <Muted>{copy(pageContract, "label.used_by")}</Muted>
          {pointsTo.length === 0 ? (
            <Muted>{copy(pageContract, "label.used_by_none")}</Muted>
          ) : (
            pointsTo.map((id) => (
              <Label key={id} variant="soft" color="default">
                {illnessLabel(id)}
              </Label>
            ))
          )}
          {editable && unlinked.length > 0 ? (
            <TextField
              select
              size="small"
              label={copy(pageContract, "label.pick_sign")}
              value=""
              onChange={(e) => {
                if (e.target.value) onAddToIllness(token, e.target.value);
              }}
              slotProps={{ select: { displayEmpty: true } }}
              sx={{ minWidth: 180 }}
            >
              <MenuItem value="">{copy(pageContract, "action.add_rule")}</MenuItem>
              {unlinked.map((id) => (
                <MenuItem key={id} value={id}>
                  {illnessLabel(id)}
                </MenuItem>
              ))}
            </TextField>
          ) : null}
          <Typography variant="caption" component="span" sx={{ color: "text.disabled", ml: "auto" }}>
            {copy(pageContract, "label.advanced")}: {token}
          </Typography>
        </Stack>
      ) : null}
    </Stack>
  );
}

/** The four bounds, in the words a vet uses rather than the operators SQL uses. */
const BAND_BOUNDS = [
  { bound: "gt", copyKey: "label.band_over" },
  { bound: "gte", copyKey: "label.band_from" },
  { bound: "lt", copyKey: "label.band_under" },
  { bound: "lte", copyKey: "label.band_upto" },
] as const;

function BandList({
  question,
  editable,
  onChange,
  pageContract,
}: {
  question: HealthRegisterQuestion;
  editable: boolean;
  onChange: (patch: Partial<HealthRegisterQuestion>) => void;
  pageContract: AdminUiPageContract;
}) {
  const bands = question.bands ?? [];
  const num = (raw: string): number | undefined => {
    const t = raw.trim();
    if (t === "") return undefined;
    const n = Number(t);
    // A value that is not a number is left UNSET rather than coerced to 0: a band silently
    // starting at zero would put every animal in it.
    return Number.isFinite(n) ? n : undefined;
  };
  return (
    <Stack spacing={0.75}>
      <Muted>
        {copy(pageContract, "label.findings")} · {question.unit ?? ""}
      </Muted>
      {bands.map((b, i) => (
        <Stack key={i} direction="row" spacing={1} useFlexGap sx={{ flexWrap: "wrap", alignItems: "flex-start" }}>
          {BAND_BOUNDS.map(({ bound, copyKey }) => (
            <TextField
              key={bound}
              size="small"
              label={copy(pageContract, copyKey)}
              value={b[bound] ?? ""}
              disabled={!editable}
              onChange={(e) =>
                onChange({
                  bands: bands.map((x, k) => (k === i ? { ...x, [bound]: num(e.target.value) } : x)),
                })
              }
              sx={fieldSx("0 1 100px")}
            />
          ))}
          <TextField
            size="small"
            label={copy(pageContract, "label.findings")}
            value={joinTokens(b.emits)}
            disabled={!editable}
            onChange={(e) =>
              onChange({
                bands: bands.map((x, k) => (k === i ? { ...x, emits: splitTokens(e.target.value) } : x)),
              })
            }
            slotProps={{ htmlInput: { "aria-label": `${copy(pageContract, "label.findings")} ${i + 1}` } }}
            sx={fieldSx("2 1 220px")}
          />
          {editable ? (
            <RemoveButton
              label={`${copy(pageContract, "action.remove_band")} ${i + 1}`}
              onClick={() => onChange({ bands: bands.filter((_, k) => k !== i) })}
            />
          ) : null}
        </Stack>
      ))}
      {editable ? (
        <AddButton onClick={() => onChange({ bands: [...bands, { emits: [] }] })}>
          {copy(pageContract, "action.add_band")}
        </AddButton>
      ) : null}
    </Stack>
  );
}

function RuleRow({
  rule,
  index,
  editable,
  pageContract,
  signIndex,
  choices,
  errors,
  onChange,
  onRemove,
}: {
  rule: HealthRegisterRule;
  index: number;
  editable: boolean;
  pageContract: AdminUiPageContract;
  signIndex: SignIndex;
  choices: Map<string, { token: string; questionTitle: string; answerLabel: string }[]>;
  errors: HealthConfigFieldError[];
  onChange: (patch: Partial<HealthRegisterRule>) => void;
  onRemove: () => void;
}) {
  const tiers = [
    { key: "pathognomonic" as const, label: copy(pageContract, "label.tier.pathognomonic") },
    { key: "probable" as const, label: copy(pageContract, "label.tier.probable") },
    { key: "possible" as const, label: copy(pageContract, "label.tier.possible") },
  ];
  const total = tiers.reduce((n, t) => n + (rule[t.key] ?? []).length, 0);

  return (
    <Stack spacing={1.25} sx={{ ...BLOCK_SX, p: 2, borderColor: errors.length ? "error.main" : "divider" }}>
      {rule.id ? <Typography variant="subtitle1" component="h4">{illnessLabel(rule.id)}</Typography> : null}
      <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: "wrap", alignItems: "flex-start" }}>
        <TextField
          size="small"
          label={copy(pageContract, "label.rule_id")}
          value={rule.id}
          disabled={!editable}
          onChange={(e) => onChange({ id: e.target.value })}
          sx={fieldSx("1 1 180px")}
        />
        <TextField
          size="small"
          label={copy(pageContract, "label.treats")}
          value={rule.treats ?? ""}
          disabled={!editable}
          placeholder={copy(pageContract, "label.field_action")}
          onChange={(e) => onChange({ treats: e.target.value.trim() })}
          slotProps={{
            htmlInput: { "aria-label": `${copy(pageContract, "label.treats")} ${index + 1}` },
            inputLabel: { shrink: true },
          }}
          sx={fieldSx("1 1 180px")}
        />
        <TextField
          size="small"
          label={copy(pageContract, "label.severity")}
          value={rule.severity_base ?? ""}
          disabled={!editable}
          onChange={(e) => {
            const t = e.target.value.trim();
            const n = Number(t);
            onChange({ severity_base: t === "" || !Number.isFinite(n) ? undefined : n });
          }}
          sx={fieldSx("0 1 120px")}
        />
        {editable ? <RemoveButton label={copy(pageContract, "action.remove_rule")} onClick={onRemove} /> : null}
      </Stack>

      {total === 0 ? (
        <Muted>{copy(pageContract, "label.no_conditions")}</Muted>
      ) : null}

      {tiers.map(({ key, label }) => (
        <TierBlock
          key={key}
          tierKey={key}
          label={label}
          clauses={rule[key] ?? []}
          editable={editable}
          pageContract={pageContract}
          signIndex={signIndex}
          choices={choices}
          onChange={(clauses) => onChange({ [key]: clauses } as Partial<HealthRegisterRule>)}
        />
      ))}

      <RowErrors errors={errors} />
    </Stack>
  );
}

/**
 * One confidence tier of one illness.
 *
 * Each line is ONE WAY to recognise the illness and any single line is enough, so the
 * lines read as alternatives and the signs INSIDE a line read as "and". That is exactly
 * how the engine evaluates them -- clauses are OR, findings within a clause are AND --
 * and saying it in words is the difference between a screen a vet can check and a screen
 * they have to be taught.
 */
function TierBlock({
  tierKey,
  label,
  clauses,
  editable,
  pageContract,
  signIndex,
  choices,
  onChange,
}: {
  tierKey: "pathognomonic" | "probable" | "possible";
  label: string;
  clauses: { findings: string[]; residual?: boolean }[];
  editable: boolean;
  pageContract: AdminUiPageContract;
  signIndex: SignIndex;
  choices: Map<string, { token: string; questionTitle: string; answerLabel: string }[]>;
  onChange: (clauses: { findings: string[]; residual?: boolean }[]) => void;
}) {
  if (clauses.length === 0 && !editable) return null;
  return (
    <Stack spacing={0.5}>
      <Muted>
        {label}
        {clauses.length > 1 ? ` — ${copy(pageContract, "label.any_one_confirms")}` : ""}
      </Muted>
      {clauses.map((clause, i) => (
        <Stack
          key={`${tierKey}-${i}`}
          direction="row"
          spacing={0.75}
          useFlexGap
          sx={{ flexWrap: "wrap", alignItems: "center", pl: 0.5 }}
        >
          {(clause.findings ?? []).map((token, k) => (
            <Box key={`${token}-${k}`} component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 0.5 }}>
              {k > 0 ? (
                <Muted>{copy(pageContract, "label.all_must_hold")}</Muted>
              ) : null}
              <Chip
                size="small"
                variant="soft"
                title={token}
                label={signLabel(token, signIndex)}
                onDelete={
                  editable
                    ? () => {
                        const next = clause.findings.filter((_, j) => j !== k);
                        onChange(
                          next.length === 0
                            ? clauses.filter((_, j) => j !== i)
                            : clauses.map((c, j) => (j === i ? { ...c, findings: next } : c)),
                        );
                      }
                    : undefined
                }
                deleteIcon={
                  <Iconify icon="solar:close-circle-bold" aria-label={`${copy(pageContract, "action.remove_clause")} ${k + 1}`} />
                }
              />
            </Box>
          ))}
          {editable ? (
            <SignPicker
              pageContract={pageContract}
              choices={choices}
              onPick={(token) =>
                onChange(
                  clauses.map((c, j) =>
                    j === i ? { ...c, findings: [...(c.findings ?? []), token] } : c,
                  ),
                )
              }
            />
          ) : null}
        </Stack>
      ))}
      {editable ? (
        <SignPicker
          pageContract={pageContract}
          choices={choices}
          addLabel={copy(pageContract, "action.add_clause")}
          onPick={(token) => onChange([...clauses, { findings: [token] }])}
        />
      ) : null}
    </Stack>
  );
}

/**
 * Pick a sign by the question and answer it comes from.
 *
 * Grouped by question, because that is how the manager meets them on the phone and how
 * an author remembers them. A plain select rather than a search box: 34 questions is a
 * list you scan, not one you have to query.
 */
function SignPicker({
  pageContract,
  choices,
  onPick,
  addLabel,
}: {
  pageContract: AdminUiPageContract;
  choices: Map<string, { token: string; questionTitle: string; answerLabel: string }[]>;
  onPick: (token: string) => void;
  addLabel?: string;
}) {
  return (
    <TextField
      select
      size="small"
      label={addLabel ?? copy(pageContract, "label.pick_sign")}
      value=""
      onChange={(e) => {
        if (e.target.value) onPick(e.target.value);
      }}
      slotProps={{ select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 360 } } } } } }}
      sx={{ minWidth: 180, maxWidth: 260 }}
    >
      <MenuItem value="">{addLabel ?? copy(pageContract, "label.pick_sign")}</MenuItem>
      {/* Grouped by question: MUI Select reads options as direct children, so each group is a
          ListSubheader followed by its answers in one flat list (was a native optgroup). */}
      {[...choices.entries()].flatMap(([questionId, rows]) => [
        <ListSubheader key={`q-${questionId}`}>{rows[0]?.questionTitle || questionId}</ListSubheader>,
        ...rows.map((row) => (
          <MenuItem key={row.token} value={row.token}>
            {row.answerLabel}
          </MenuItem>
        )),
      ])}
    </TextField>
  );
}

function RowErrors({ errors }: { errors: HealthConfigFieldError[] }) {
  if (errors.length === 0) return null;
  return (
    <Box component="ul" sx={{ typography: "body2", m: 0, pl: 2.25, color: "error.main" }}>
      {errors.map((e, i) => (
        <li key={`${e.field}-${i}`}>{e.message}</li>
      ))}
    </Box>
  );
}

/**
 * The verdict a publish would apply, shown WHILE EDITING rather than only on the button.
 *
 * It lists the blocking problems as well as the allowed ones. Showing only the warnings
 * left an author with a draft that could not publish and a panel that said nothing about
 * why -- they found out by pressing Publish, which is the opposite of what a verdict on
 * the read is for.
 */
export function RegisterProblems({
  problems,
  pageContract,
  heading,
  note,
}: {
  problems: HealthRegisterProblem[] | undefined;
  pageContract: AdminUiPageContract;
  heading: string;
  note?: string;
}) {
  const blocking = useMemo(() => (problems ?? []).filter((p) => p.fatal), [problems]);
  const allowed = useMemo(() => (problems ?? []).filter((p) => !p.fatal), [problems]);
  if (blocking.length === 0 && allowed.length === 0) return null;
  return (
    <Card component="section" sx={{ mb: 2 }}>
      <CardHeader title={heading} />
      <CardContent>
        {note ? (
          <Typography variant="body2" sx={{ color: "text.secondary", mb: 1.5 }}>
            {note}
          </Typography>
        ) : null}
        {blocking.length > 0 ? (
          <Box component="ul" sx={{ typography: "body2", mt: 0, mb: 1.25, pl: 2.25, color: "error.main" }}>
            {blocking.map((p, i) => (
              <li key={`${p.path}-${i}`}>{p.message}</li>
            ))}
          </Box>
        ) : null}
        <Box component="ul" sx={{ typography: "body2", m: 0, pl: 2.25, color: "text.secondary" }}>
          {allowed.map((p, i) => (
            <li key={`${p.path}-${i}`}>{p.message}</li>
          ))}
        </Box>
      </CardContent>
      <span hidden>{copy(pageContract, "label.warnings")}</span>
    </Card>
  );
}

function ResultBand({
  result,
  pageContract,
}: {
  result: HealthRegisterActionResult | null;
  pageContract: AdminUiPageContract;
}) {
  if (!result) return null;
  const text = optionalCopy(pageContract, result.messageKey) ?? result.detail ?? "";
  return (
    // The Alert's own severity icon carries the warning glyph (no second warning icon inside it).
    <Alert severity={result.ok ? "success" : "error"} sx={{ mb: 2 }}>
      <div>
        <Typography variant="subtitle2" component="div">{text}</Typography>
        {result.detail && !result.ok ? (
          <Typography variant="body2" component="div" sx={{ color: "text.secondary" }}>{result.detail}</Typography>
        ) : null}
        {result.warnings && result.warnings.length > 0 ? (
          <Box component="ul" sx={{ typography: "body2", color: "text.secondary", mt: 0.75, mb: 0, pl: 2.25 }}>
            {result.warnings.map((w, i) => (
              <li key={`${w.path}-${i}`}>{w.message}</li>
            ))}
          </Box>
        ) : null}
      </div>
    </Alert>
  );
}
