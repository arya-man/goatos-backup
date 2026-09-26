"use client";

import { useRouter } from "next/navigation";
import { useCallback, useMemo, useRef, useState, useTransition } from "react";
import { AlertTriangle, Plus, Trash2, X } from "lucide-react";

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
        <Alert severity="error" style={{ marginBottom: 16 }}><div>{disabledReason}</div>
        </Alert>
      ) : null}

      <RegisterProblems
        problems={detail.problems ?? undefined}
        pageContract={pageContract}
        heading={copy(pageContract, "label.warnings")}
        note={optionalCopy(pageContract, "note.register_warnings")}
      />

      <div className="wftoolbar" style={{ gap: 8, marginBottom: 12 }}>
        <button
          type="button"
          className={open === "questions" ? "btn" : "btn ghost"}
          onClick={() => setOpen("questions")}
        >
          {copy(pageContract, "section.questions.title")} ({doc.questions.length})
        </button>
        <button
          type="button"
          className={open === "rules" ? "btn" : "btn ghost"}
          onClick={() => setOpen("rules")}
        >
          {copy(pageContract, "section.rules.title")} ({doc.rules.length})
        </button>
        <div className="sp" style={{ flex: 1 }} />
        <button type="button" className="btn" onClick={onSave} disabled={!editable || pending}>
          {copy(pageContract, "action.save_draft")}
        </button>
        <button type="button" className="btn primary" onClick={onPublish} disabled={!editable || pending}>
          {copy(pageContract, "action.publish_register")}
        </button>
        <button type="button" className="btn ghost" onClick={onDiscard} disabled={!editable || pending}>
          {copy(pageContract, "action.discard_register_draft")}
        </button>
      </div>

      {open === "questions" ? (
        <section className="card" style={{ marginBottom: 16 }}>
          <div className="hd">
            <h3>{copy(pageContract, "section.questions.title")}</h3>
            <span className="small muted">{copy(pageContract, "section.questions.caption")}</span>
          </div>
          <Caption>{copy(pageContract, "note.questions_how")}</Caption>
          <div className="bd" style={{ display: "grid", gap: 12 }}>
            {doc.questions.length === 0 ? (
              <div className="muted small" style={{ padding: "18px 4px", textAlign: "center" }}>
                {copy(pageContract, "empty.questions")}
              </div>
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
              <button
                type="button"
                className="btn ghost"
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
                <Plus className="ic" aria-hidden="true" /> {copy(pageContract, "action.add_question")}
              </button>
            ) : null}
          </div>
        </section>
      ) : (
        <section className="card" style={{ marginBottom: 16 }}>
          <div className="hd">
            <h3>{copy(pageContract, "section.rules.title")}</h3>
            <span className="small muted">{copy(pageContract, "section.rules.caption")}</span>
          </div>
          <Caption>{copy(pageContract, "note.rules_how")}</Caption>
          <div className="bd" style={{ display: "grid", gap: 12 }}>
            {doc.rules.length === 0 ? (
              <div className="muted small" style={{ padding: "18px 4px", textAlign: "center" }}>
                {copy(pageContract, "empty.rules")}
              </div>
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
              <button
                type="button"
                className="btn ghost"
                onClick={() =>
                  setDoc((d) => ({
                    ...d,
                    rules: [...d.rules, { id: "", severity_base: 2, pathognomonic: [{ findings: [] }] }],
                  }))
                }
              >
                <Plus className="ic" aria-hidden="true" /> {copy(pageContract, "action.add_rule")}
              </button>
            ) : null}
          </div>
        </section>
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
    <div className="card" style={{ margin: 0, borderColor: errors.length ? "var(--danger)" : undefined }}>
      <div className="bd" style={{ display: "grid", gap: 8 }}>
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "flex-end" }}>
          <label className="fld" style={{ flex: "2 1 240px" }}>
            <span>{copy(pageContract, "label.question_title")}</span>
            <input
              value={question.title}
              disabled={!editable}
              onChange={(e) => onChange({ title: e.target.value })}
              aria-label={`${copy(pageContract, "label.question_title")} ${index + 1}`}
            />
          </label>
          <label className="fld" style={{ flex: "1 1 140px" }}>
            <span>{copy(pageContract, "label.question_id")}</span>
            <input value={question.id} disabled={!editable} onChange={(e) => onChange({ id: e.target.value })} />
          </label>
          <label className="fld" style={{ flex: "1 1 150px" }}>
            <span>{copy(pageContract, "label.question_kind")}</span>
            <select
              value={question.kind}
              disabled={!editable}
              onChange={(e) => onChange({ kind: e.target.value as HealthRegisterQuestion["kind"] })}
            >
              <option value="choice">{copy(pageContract, "label.kind.choice")}</option>
              <option value="multi">{copy(pageContract, "label.kind.multi")}</option>
              <option value="number">{copy(pageContract, "label.kind.number")}</option>
            </select>
          </label>
          {editable ? (
            <button type="button" className="btn ghost" onClick={onRemove} aria-label={copy(pageContract, "action.remove_question")}>
              <Trash2 className="ic" aria-hidden="true" />
            </button>
          ) : null}
        </div>

        {question.kind === "number" ? (
          <BandList question={question} editable={editable} onChange={onChange} pageContract={pageContract} />
        ) : (
          <div style={{ display: "grid", gap: 6 }}>
            <div className="small muted">{copy(pageContract, "label.answers")}</div>
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
              <button
                type="button"
                className="btn ghost"
                onClick={() => onChange({ options: [...options, { value: "", label: "", emits: [] }] })}
              >
                <Plus className="ic" aria-hidden="true" /> {copy(pageContract, "action.add_answer")}
              </button>
            ) : null}
          </div>
        )}

        <RowErrors errors={errors} />
      </div>
    </div>
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
    <div
      style={{
        display: "grid",
        gap: 6,
        padding: "8px 10px",
        borderRadius: 8,
        border: "1px solid var(--line)",
      }}
    >
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
        <input
          style={{ flex: "1 1 180px" }}
          value={option.label}
          disabled={!editable}
          onChange={(e) => onChange({ label: e.target.value })}
          aria-label={`${copy(pageContract, "label.answer_label")} ${index + 1}`}
        />
        <FormControlLabel className="small" disabled={!editable} control={<Checkbox checked={isSign} disabled={!editable} onChange={(e) =>
              onChange({
                // Ticking DERIVES the token; unticking drops it. An author never types one,
                // and an answer that means nothing is wrong carries none -- which is what
                // keeps "Eating normally" out of the evidence the engine reasons over.
                emits: e.target.checked ? [derivedSign(question, option.value)] : [],
              })
            } sx={{ p: { xs: 1.5, sm: 1 } }} />} label={<>{copy(pageContract, isSign ? "label.is_a_sign" : "label.not_a_sign")}</>} />
        {editable ? (
          <button
            type="button"
            className="btn ghost"
            aria-label={`${copy(pageContract, "action.remove_answer")} ${index + 1}`}
            onClick={onRemove}
          >
            <Trash2 className="ic" aria-hidden="true" />
          </button>
        ) : null}
      </div>

      {isSign ? (
        <div style={{ display: "flex", gap: 6, flexWrap: "wrap", alignItems: "center" }}>
          <span className="small muted">{copy(pageContract, "label.used_by")}</span>
          {pointsTo.length === 0 ? (
            <span className="small muted">{copy(pageContract, "label.used_by_none")}</span>
          ) : (
            pointsTo.map((id) => (
              <span key={id} className="tag">
                {illnessLabel(id)}
              </span>
            ))
          )}
          {editable && unlinked.length > 0 ? (
            <select
              value=""
              aria-label={copy(pageContract, "label.pick_sign")}
              onChange={(e) => {
                if (e.target.value) onAddToIllness(token, e.target.value);
              }}
            >
              <option value="">{copy(pageContract, "action.add_rule")}</option>
              {unlinked.map((id) => (
                <option key={id} value={id}>
                  {illnessLabel(id)}
                </option>
              ))}
            </select>
          ) : null}
          <span className="small muted" style={{ marginLeft: "auto", opacity: 0.6 }}>
            {copy(pageContract, "label.advanced")}: {token}
          </span>
        </div>
      ) : null}
    </div>
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
    <div style={{ display: "grid", gap: 6 }}>
      <div className="small muted">
        {copy(pageContract, "label.findings")} · {question.unit ?? ""}
      </div>
      {bands.map((b, i) => (
        <div key={i} style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          {BAND_BOUNDS.map(({ bound, copyKey }) => (
            <label key={bound} className="fld" style={{ flex: "0 1 100px" }}>
              <span>{copy(pageContract, copyKey)}</span>
              <input
                value={b[bound] ?? ""}
                disabled={!editable}
                onChange={(e) =>
                  onChange({
                    bands: bands.map((x, k) => (k === i ? { ...x, [bound]: num(e.target.value) } : x)),
                  })
                }
              />
            </label>
          ))}
          <input
            style={{ flex: "2 1 220px" }}
            value={joinTokens(b.emits)}
            disabled={!editable}
            placeholder={copy(pageContract, "label.findings")}
            onChange={(e) =>
              onChange({
                bands: bands.map((x, k) => (k === i ? { ...x, emits: splitTokens(e.target.value) } : x)),
              })
            }
            aria-label={`${copy(pageContract, "label.findings")} ${i + 1}`}
          />
          {editable ? (
            <button
              type="button"
              className="btn ghost"
              aria-label={`${copy(pageContract, "action.remove_band")} ${i + 1}`}
              onClick={() => onChange({ bands: bands.filter((_, k) => k !== i) })}
            >
              <Trash2 className="ic" aria-hidden="true" />
            </button>
          ) : null}
        </div>
      ))}
      {editable ? (
        <button
          type="button"
          className="btn ghost"
          onClick={() => onChange({ bands: [...bands, { emits: [] }] })}
        >
          <Plus className="ic" aria-hidden="true" /> {copy(pageContract, "action.add_band")}
        </button>
      ) : null}
    </div>
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
    <div className="card" style={{ margin: 0, borderColor: errors.length ? "var(--danger)" : undefined }}>
      <div className="bd" style={{ display: "grid", gap: 10 }}>
        {rule.id ? <h4 style={{ margin: 0 }}>{illnessLabel(rule.id)}</h4> : null}
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "flex-end" }}>
          <label className="fld" style={{ flex: "1 1 180px" }}>
            <span>{copy(pageContract, "label.rule_id")}</span>
            <input value={rule.id} disabled={!editable} onChange={(e) => onChange({ id: e.target.value })} />
          </label>
          <label className="fld" style={{ flex: "1 1 180px" }}>
            <span>{copy(pageContract, "label.treats")}</span>
            <input
              value={rule.treats ?? ""}
              disabled={!editable}
              placeholder={copy(pageContract, "label.field_action")}
              onChange={(e) => onChange({ treats: e.target.value.trim() })}
              aria-label={`${copy(pageContract, "label.treats")} ${index + 1}`}
            />
          </label>
          <label className="fld" style={{ flex: "0 1 120px" }}>
            <span>{copy(pageContract, "label.severity")}</span>
            <input
              value={rule.severity_base ?? ""}
              disabled={!editable}
              onChange={(e) => {
                const t = e.target.value.trim();
                const n = Number(t);
                onChange({ severity_base: t === "" || !Number.isFinite(n) ? undefined : n });
              }}
            />
          </label>
          {editable ? (
            <button
              type="button"
              className="btn ghost"
              onClick={onRemove}
              aria-label={copy(pageContract, "action.remove_rule")}
            >
              <Trash2 className="ic" aria-hidden="true" />
            </button>
          ) : null}
        </div>

        {total === 0 ? (
          <div className="small muted">{copy(pageContract, "label.no_conditions")}</div>
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
      </div>
    </div>
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
    <div style={{ display: "grid", gap: 4 }}>
      <div className="small muted">
        {label}
        {clauses.length > 1 ? ` — ${copy(pageContract, "label.any_one_confirms")}` : ""}
      </div>
      {clauses.map((clause, i) => (
        <div
          key={`${tierKey}-${i}`}
          style={{ display: "flex", gap: 6, flexWrap: "wrap", alignItems: "center", paddingLeft: 4 }}
        >
          {(clause.findings ?? []).map((token, k) => (
            <span key={`${token}-${k}`} style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
              {k > 0 ? (
                <span className="small muted">{copy(pageContract, "label.all_must_hold")}</span>
              ) : null}
              <span className="tag" title={token}>
                {signLabel(token, signIndex)}
                {editable ? (
                  <button
                    type="button"
                    className="btn ghost sm"
                    aria-label={`${copy(pageContract, "action.remove_clause")} ${k + 1}`}
                    style={{ marginLeft: 4, padding: 0, lineHeight: 1 }}
                    onClick={() => {
                      const next = clause.findings.filter((_, j) => j !== k);
                      onChange(
                        next.length === 0
                          ? clauses.filter((_, j) => j !== i)
                          : clauses.map((c, j) => (j === i ? { ...c, findings: next } : c)),
                      );
                    }}
                  >
                    <X className="ic" aria-hidden="true" style={{ width: 12 }} />
                  </button>
                ) : null}
              </span>
            </span>
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
        </div>
      ))}
      {editable ? (
        <SignPicker
          pageContract={pageContract}
          choices={choices}
          addLabel={copy(pageContract, "action.add_clause")}
          onPick={(token) => onChange([...clauses, { findings: [token] }])}
        />
      ) : null}
    </div>
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
    <select
      value=""
      aria-label={addLabel ?? copy(pageContract, "label.pick_sign")}
      onChange={(e) => {
        if (e.target.value) onPick(e.target.value);
        e.target.value = "";
      }}
      style={{ maxWidth: 260 }}
    >
      <option value="">{addLabel ?? copy(pageContract, "label.pick_sign")}</option>
      {[...choices.entries()].map(([questionId, rows]) => (
        <optgroup key={questionId} label={rows[0]?.questionTitle || questionId}>
          {rows.map((row) => (
            <option key={row.token} value={row.token}>
              {row.answerLabel}
            </option>
          ))}
        </optgroup>
      ))}
    </select>
  );
}

function RowErrors({ errors }: { errors: HealthConfigFieldError[] }) {
  if (errors.length === 0) return null;
  return (
    <ul className="small" style={{ margin: 0, paddingLeft: 18, color: "var(--danger)" }}>
      {errors.map((e, i) => (
        <li key={`${e.field}-${i}`}>{e.message}</li>
      ))}
    </ul>
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
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <h3>{heading}</h3>
      </div>
      <div className="bd">
        {note ? (
          <p className="small muted" style={{ marginTop: 0, lineHeight: 1.6 }}>
            {note}
          </p>
        ) : null}
        {blocking.length > 0 ? (
          <ul className="small" style={{ margin: "0 0 10px", paddingLeft: 18, lineHeight: 1.7, color: "var(--danger)" }}>
            {blocking.map((p, i) => (
              <li key={`${p.path}-${i}`}>{p.message}</li>
            ))}
          </ul>
        ) : null}
        <ul className="small muted" style={{ margin: 0, paddingLeft: 18, lineHeight: 1.7 }}>
          {allowed.map((p, i) => (
            <li key={`${p.path}-${i}`}>{p.message}</li>
          ))}
        </ul>
      </div>
      <span hidden>{copy(pageContract, "label.warnings")}</span>
    </section>
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
    <Alert severity={result.ok ? "success" : "error"} style={{ marginBottom: 16 }}>
      {result.ok ? null : <AlertTriangle className="ic" aria-hidden="true" />}
      <div>
        <b>{text}</b>
        {result.detail && !result.ok ? <div className="small muted">{result.detail}</div> : null}
        {result.warnings && result.warnings.length > 0 ? (
          <ul className="small muted" style={{ margin: "6px 0 0", paddingLeft: 18 }}>
            {result.warnings.map((w, i) => (
              <li key={`${w.path}-${i}`}>{w.message}</li>
            ))}
          </ul>
        ) : null}
      </div>
    </Alert>
  );
}
