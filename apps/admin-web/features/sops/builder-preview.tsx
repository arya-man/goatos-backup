"use client";

import { useMemo, useState } from "react";
import Alert from "@mui/material/Alert";
import Button from "@mui/material/Button";
import Checkbox from "@mui/material/Checkbox";
import Chip from "@mui/material/Chip";
import FormControlLabel from "@mui/material/FormControlLabel";
import InputAdornment from "@mui/material/InputAdornment";
import Radio from "@mui/material/Radio";
import Stack from "@mui/material/Stack";
import MuiTextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";
import { Label } from "@/components/minimal/label";
import { UploadFile } from "@/components/app/upload-file";
import { EDITOR_ICON, Hint, NumBadge, Spacer } from "./editor-parts";
import { fieldConfigKind, type BuilderCondition, type BuilderStep } from "./sop-derive";
import { copy, optionalOptionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";

type Answer = string | string[] | undefined;

function isEmpty(v: Answer): boolean {
  return v === undefined || v === "" || (Array.isArray(v) && v.length === 0);
}

// Client mirror of the backend condition evaluator (sop/app/service.go conditionMatches), enough to make
// the preview behave: a conditionally-shown question appears/disappears as its referenced answer changes.
// Select/multiselect answers are the chosen labels, so the author's typed condition value matches by label.
function conditionMet(cond: BuilderCondition, steps: BuilderStep[], answers: Record<string, Answer>): boolean {
  const refIdx = steps.findIndex((s) => s.id === cond.refId);
  if (refIdx < 0) return true;
  const a = answers[cond.refId];
  switch (cond.operator) {
    case "answered":
      return !isEmpty(a);
    case "not_answered":
      return isEmpty(a);
    case "equals":
      return String(a ?? "") === cond.value;
    case "not_equals":
      return String(a ?? "") !== cond.value;
    case "is_one_of":
      return cond.value.split(",").map((x) => x.trim()).includes(String(a ?? ""));
    case "gt":
      return Number(a) > Number(cond.value);
    case "gte":
      return Number(a) >= Number(cond.value);
    case "lt":
      return Number(a) < Number(cond.value);
    case "lte":
      return Number(a) <= Number(cond.value);
    default:
      return true;
  }
}

// Interactive operator preview of the form being authored — updated live as questions change, and
// FILLABLE: type/select/toggle answers and conditional questions reveal or hide exactly as they will for
// a real operator. Purely local (no save, no backend); pickers/scan/proof render as disabled stubs.
// Template parts only (TextField, Radio/Checkbox lines, Chips, the template upload area); the
// data-testid hooks (pv-*) are what the builder e2e reads.
export function BuilderPreview({ pc, steps }: { pc: AdminUiPageContract; steps: BuilderStep[] }) {
  const yesNo = optionalOptionGroup(pc, "yes_no");
  const [answers, setAnswers] = useState<Record<string, Answer>>({});
  const [files, setFiles] = useState<Record<string, string>>({});
  const setFile = (id: string, name: string | undefined) => setFiles((prev) => ({ ...prev, [id]: name ?? "" }));

  const setAnswer = (id: string, value: Answer) => setAnswers((prev) => ({ ...prev, [id]: value }));
  const toggleMulti = (id: string, value: string) =>
    setAnswers((prev) => {
      const cur = Array.isArray(prev[id]) ? (prev[id] as string[]) : [];
      return { ...prev, [id]: cur.includes(value) ? cur.filter((v) => v !== value) : [...cur, value] };
    });

  // A question shows when it has no condition, or its condition (against an EARLIER answer) is met.
  const visibleIds = useMemo(() => {
    const set = new Set<string>();
    steps.forEach((s, i) => {
      const cond = s.visibleWhen;
      const refIdx = cond ? steps.findIndex((x) => x.id === cond.refId) : -1;
      const show = !cond || refIdx < 0 || refIdx >= i ? true : conditionMet(cond, steps, answers);
      if (show) set.add(s.id);
    });
    return set;
  }, [steps, answers]);

  if (steps.length === 0) {
    return <Alert severity="info">{copy(pc, "builder.preview.empty")}</Alert>;
  }

  const visibleSteps = steps.filter((s) => visibleIds.has(s.id));

  return (
    <Stack spacing={2} data-testid="pv-wrap" sx={{ minWidth: 0 }}>
      <Stack direction="row" spacing={1} sx={{ alignItems: "center" }}>
        <Hint caption>{copy(pc, "builder.preview.subtitle")}</Hint>
        <Spacer />
        <Button
          size="small"
          color="inherit"
          variant="text"
          startIcon={<Iconify icon="solar:restart-bold" />}
          onClick={() => {
            setAnswers({});
            setFiles({});
          }}
          disabled={Object.keys(answers).length === 0 && Object.keys(files).length === 0}
        >
          {copy(pc, "builder.preview.reset")}
        </Button>
      </Stack>
      <Stack spacing={2.5} data-testid="pv-form">
        {visibleSteps.map((step) => {
          const kind = fieldConfigKind(step.type);
          const answer = answers[step.id];
          const num = steps.findIndex((s) => s.id === step.id) + 1;
          const opts = step.options.filter((o) => o.label.trim());
          const label = step.label.trim() || `${copy(pc, "builder.question_label")} ${num}`;
          return (
            <Stack spacing={1} key={step.id} data-testid="pv-field" sx={{ minWidth: 0 }}>
              <Stack direction="row" spacing={1} useFlexGap sx={{ alignItems: "center", flexWrap: "wrap" }}>
                <NumBadge>{num}</NumBadge>
                <Typography variant="subtitle2" component="span" sx={{ minWidth: 0 }}>
                  {label}
                </Typography>
                {step.required ? <Label color="warning">{copy(pc, "builder.preview.required_badge")}</Label> : null}
                {step.visibleWhen ? <Label color="info">{copy(pc, "builder.preview.conditional_badge")}</Label> : null}
              </Stack>
              {step.helpText.trim() ? <Hint caption>{step.helpText}</Hint> : null}

              {kind === "text" ? (
                <MuiTextField
                  fullWidth
                  size="small"
                  multiline={step.longText}
                  minRows={step.longText ? 2 : undefined}
                  placeholder={step.placeholder}
                  value={(answer as string) ?? ""}
                  slotProps={{ htmlInput: { "aria-label": label, "data-testid": "pv-ctl" } }}
                  onChange={(e) => setAnswer(step.id, e.target.value)}
                />
              ) : null}

              {kind === "number" ? (
                <MuiTextField
                  size="small"
                  type="number"
                  value={(answer as string) ?? ""}
                  slotProps={{
                    htmlInput: { "aria-label": label, "data-testid": "pv-ctl" },
                    input: step.unit.trim() ? { endAdornment: <InputAdornment position="end">{step.unit}</InputAdornment> } : undefined,
                  }}
                  onChange={(e) => setAnswer(step.id, e.target.value)}
                />
              ) : null}

              {kind === "boolean" ? (
                <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: "wrap" }}>
                  {yesNo.map((o) => (
                    <Chip
                      key={o.key}
                      label={o.label}
                      clickable
                      color={answer === o.key ? "primary" : "default"}
                      variant={answer === o.key ? "filled" : "outlined"}
                      aria-pressed={answer === o.key}
                      onClick={() => setAnswer(step.id, answer === o.key ? undefined : o.key)}
                    />
                  ))}
                </Stack>
              ) : null}

              {kind === "options" ? (
                opts.length === 0 ? (
                  <Hint caption>{copy(pc, "builder.options.empty")}</Hint>
                ) : (
                  <Stack data-testid="pv-opts">
                    {opts.map((o) => {
                      const multi = step.type === "multiselect";
                      const checked = multi ? Array.isArray(answer) && answer.includes(o.label) : answer === o.label;
                      const onClick = () => (multi ? toggleMulti(step.id, o.label) : setAnswer(step.id, answer === o.label ? undefined : o.label));
                      return (
                        <FormControlLabel
                          key={o.id}
                          data-testid={multi ? "pv-check" : "pv-radio"}
                          data-checked={checked ? "1" : "0"}
                          control={multi ? <Checkbox checked={checked} onChange={onClick} /> : <Radio checked={checked} onClick={onClick} />}
                          label={o.label}
                          sx={{ mr: 0, "& .MuiFormControlLabel-label": { typography: "body2" } }}
                        />
                      );
                    })}
                  </Stack>
                )
              ) : null}

              {/* Pickers render as a live-list dropdown stub, scan as a scan control, proof as the
                  template upload area — the real operator control's look (inert in preview). */}
              {kind === "picker" ? (
                <MuiTextField
                  disabled
                  fullWidth
                  size="small"
                  value={copy(pc, "builder.picker.note")}
                  data-testid="pv-selectstub"
                  slotProps={{ htmlInput: { "aria-disabled": true }, input: { endAdornment: <Iconify icon={EDITOR_ICON.down} /> } }}
                />
              ) : null}
              {kind === "scan" ? (
                step.multiScan ? (
                  // Multi-scan: operator scans every goat in the shed — a growing multi-select of tags.
                  <Stack spacing={1} data-testid="pv-scanmulti">
                    <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: "wrap", alignItems: "center" }}>
                      {(Array.isArray(answer) ? answer : []).map((g, gi) => (
                        <Chip key={`${g}-${gi}`} size="small" label={g} data-testid="pv-chip" onDelete={() => setAnswer(step.id, (answer as string[]).filter((_, k) => k !== gi))} />
                      ))}
                      <Button
                        size="small"
                        variant="outlined"
                        color="primary"
                        startIcon={<Iconify icon="solar:tag-horizontal-bold-duotone" />}
                        onClick={() => {
                          const cur = Array.isArray(answer) ? (answer as string[]) : [];
                          setAnswer(step.id, [...cur, `#${cur.length + 1}`]);
                        }}
                      >
                        {copy(pc, "builder.preview.scan_add")}
                      </Button>
                    </Stack>
                    <Hint caption>{copy(pc, "builder.scan.note")}</Hint>
                  </Stack>
                ) : (
                  <MuiTextField
                    disabled
                    fullWidth
                    size="small"
                    value={copy(pc, "builder.scan.note")}
                    data-testid="pv-selectstub"
                    slotProps={{
                      htmlInput: { "aria-disabled": true },
                      input: {
                        startAdornment: <Iconify icon="solar:tag-horizontal-bold-duotone" sx={{ mr: 1 }} />,
                        endAdornment: <Iconify icon={EDITOR_ICON.down} />,
                      },
                    }}
                  />
                )
              ) : null}
              {kind === "proof" ? (
                // Real native file picker (hidden inside the template upload area) so the author can
                // verify the upload affordance. Preview only keeps the chosen filename; no upload.
                <UploadFile
                  testId="pv-drop"
                  accept={step.type === "photo_proof" ? "image/*" : "video/*"}
                  title={copy(pc, "builder.proof.note")}
                  description={files[step.id] || undefined}
                  ariaLabel={label}
                  onFileChange={(file) => setFile(step.id, file?.name)}
                />
              ) : null}
            </Stack>
          );
        })}
      </Stack>
    </Stack>
  );
}
