"use client";

import { useRouter } from "next/navigation";
import { useCallback, useMemo, useRef, useState, useTransition } from "react";
import { AlertTriangle, Plus, Trash2 } from "lucide-react";

import { copy, optionalCopy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
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
  discardRegisterDraft,
  publishRegisterDraft,
  saveRegisterDraft,
  type HealthRegisterActionResult,
} from "./health-register-actions";

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
type OnWritten = (result: HealthRegisterActionResult) => void;

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

  const isDraft = detail.status === "draft";
  const editable = mayWrite && isDraft;
  const fieldErrors = result && !result.ok ? result.fieldErrors : undefined;

  const run = useCallback(
    (fn: RegisterWrite, onSuccess?: OnWritten) => {
      startTransition(async () => {
        const r = await fn();
        setResult(r);
        if (r.ok) onSuccess?.(r);
        router.refresh();
      });
    },
    [router],
  );

  const onSave = () => {
    if (!saveKey.current) saveKey.current = mintKey("register-save");
    run(
      () => saveRegisterDraft(detail.animal_class, doc, saveKey.current),
      () => {
        saveKey.current = "";
      },
    );
  };

  const onPublish = () => {
    if (!publishKey.current) publishKey.current = mintKey("register-publish");
    run(
      () => publishRegisterDraft(detail.register_version_id, publishKey.current),
      () => {
        publishKey.current = "";
        router.push(listHref);
      },
    );
  };

  const onDiscard = () => {
    if (!discardKey.current) discardKey.current = mintKey("register-discard");
    run(
      () => discardRegisterDraft(detail.register_version_id, discardKey.current),
      () => {
        discardKey.current = "";
        router.push(listHref);
      },
    );
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
        <div className="alert" style={{ marginBottom: 16 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div>{disabledReason}</div>
        </div>
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
          <p className="small muted" style={{ margin: "0 14px 10px", lineHeight: 1.6 }}>
            {copy(pageContract, "section.questions.note")}
          </p>
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
                  errors={errorsFor(fieldErrors, `questions.${i}`)}
                  onChange={(patch) => patchQuestion(i, patch)}
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
                <Plus className="ic" aria-hidden="true" /> {copy(pageContract, "section.questions.title")}
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
          <p className="small muted" style={{ margin: "0 14px 10px", lineHeight: 1.6 }}>
            {copy(pageContract, "section.rules.note")}
          </p>
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
                <Plus className="ic" aria-hidden="true" /> {copy(pageContract, "section.rules.title")}
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
  errors,
  onChange,
  onRemove,
}: {
  question: HealthRegisterQuestion;
  index: number;
  editable: boolean;
  pageContract: AdminUiPageContract;
  errors: HealthConfigFieldError[];
  onChange: (patch: Partial<HealthRegisterQuestion>) => void;
  onRemove: () => void;
}) {
  const options = question.options ?? [];
  const patchOption = (i: number, patch: Partial<HealthRegisterOption>) =>
    onChange({ options: options.map((o, k) => (k === i ? { ...o, ...patch } : o)) });

  return (
    <div className="card" style={{ margin: 0, borderColor: errors.length ? "var(--danger)" : undefined }}>
      <div className="bd" style={{ display: "grid", gap: 8 }}>
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "flex-end" }}>
          <label className="small" style={{ flex: "2 1 220px" }}>
            {copy(pageContract, "section.questions.title")}
            <input
              value={question.title}
              disabled={!editable}
              onChange={(e) => onChange({ title: e.target.value })}
              aria-label={`${copy(pageContract, "section.questions.title")} ${index + 1}`}
            />
          </label>
          <label className="small" style={{ flex: "1 1 120px" }}>
            id
            <input value={question.id} disabled={!editable} onChange={(e) => onChange({ id: e.target.value })} />
          </label>
          <label className="small" style={{ flex: "1 1 120px" }}>
            kind
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
              <div key={`${o.value}-${i}`} style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
                <input
                  style={{ flex: "1 1 140px" }}
                  value={o.label}
                  disabled={!editable}
                  onChange={(e) => patchOption(i, { label: e.target.value })}
                  aria-label={`${copy(pageContract, "label.answers")} ${i + 1}`}
                />
                <input
                  style={{ flex: "1 1 110px" }}
                  value={o.value}
                  disabled={!editable}
                  onChange={(e) => patchOption(i, { value: e.target.value })}
                  aria-label={`${copy(pageContract, "label.answers")} ${i + 1}`}
                />
                {/* The join. Empty is the common case and is correct: "normal" and "no" are the
                    absence of a sign, not a sign named "none". */}
                <input
                  style={{ flex: "2 1 220px" }}
                  value={joinTokens(o.emits)}
                  disabled={!editable}
                  placeholder={copy(pageContract, "label.findings")}
                  onChange={(e) => patchOption(i, { emits: splitTokens(e.target.value) })}
                  aria-label={`${copy(pageContract, "label.findings")} ${i + 1}`}
                />
                {editable ? (
                  <button
                    type="button"
                    className="btn ghost"
                    aria-label={`${copy(pageContract, "action.remove_answer")} ${i + 1}`}
                    onClick={() => onChange({ options: options.filter((_, k) => k !== i) })}
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
                onClick={() => onChange({ options: [...options, { value: "", label: "", emits: [] }] })}
              >
                <Plus className="ic" aria-hidden="true" /> {copy(pageContract, "label.answers")}
              </button>
            ) : null}
          </div>
        )}

        <RowErrors errors={errors} />
      </div>
    </div>
  );
}

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
          {(["gt", "gte", "lt", "lte"] as const).map((bound) => (
            <label key={bound} className="small" style={{ flex: "0 1 90px" }}>
              {bound}
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
          <Plus className="ic" aria-hidden="true" />
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
  errors,
  onChange,
  onRemove,
}: {
  rule: HealthRegisterRule;
  index: number;
  editable: boolean;
  pageContract: AdminUiPageContract;
  errors: HealthConfigFieldError[];
  onChange: (patch: Partial<HealthRegisterRule>) => void;
  onRemove: () => void;
}) {
  const tiers = [
    { key: "pathognomonic" as const, label: copy(pageContract, "label.tier.pathognomonic") },
    { key: "probable" as const, label: copy(pageContract, "label.tier.probable") },
    { key: "possible" as const, label: copy(pageContract, "label.tier.possible") },
  ];
  return (
    <div className="card" style={{ margin: 0, borderColor: errors.length ? "var(--danger)" : undefined }}>
      <div className="bd" style={{ display: "grid", gap: 8 }}>
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "flex-end" }}>
          <label className="small" style={{ flex: "1 1 160px" }}>
            id
            <input value={rule.id} disabled={!editable} onChange={(e) => onChange({ id: e.target.value })} />
          </label>
          <label className="small" style={{ flex: "1 1 160px" }}>
            {copy(pageContract, "label.treats")}
            <input
              value={rule.treats ?? ""}
              disabled={!editable}
              placeholder={copy(pageContract, "label.field_action")}
              onChange={(e) => onChange({ treats: e.target.value.trim() })}
              aria-label={`${copy(pageContract, "label.treats")} ${index + 1}`}
            />
          </label>
          <label className="small" style={{ flex: "0 1 110px" }}>
            severity
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
            <button type="button" className="btn ghost" onClick={onRemove} aria-label={copy(pageContract, "action.remove_rule")}>
              <Trash2 className="ic" aria-hidden="true" />
            </button>
          ) : null}
        </div>

        {tiers.map(({ key, label }) => {
          const clauses = rule[key] ?? [];
          return (
            <div key={key} style={{ display: "grid", gap: 4 }}>
              <div className="small muted">{label}</div>
              {clauses.map((c, i) => (
                <div key={i} style={{ display: "flex", gap: 8 }}>
                  {/* Findings within one clause are an AND: the animal must show all of them. */}
                  <input
                    style={{ flex: 1 }}
                    value={joinTokens(c.findings)}
                    disabled={!editable}
                    placeholder={copy(pageContract, "label.findings")}
                    onChange={(e) =>
                      onChange({
                        [key]: clauses.map((x, k) =>
                          k === i ? { ...x, findings: splitTokens(e.target.value) } : x,
                        ),
                      } as Partial<HealthRegisterRule>)
                    }
                    aria-label={`${label} ${i + 1}`}
                  />
                  {editable ? (
                    <button
                      type="button"
                      className="btn ghost"
                      aria-label={`${copy(pageContract, "action.remove_clause")} ${i + 1}`}
                      onClick={() =>
                        onChange({ [key]: clauses.filter((_, k) => k !== i) } as Partial<HealthRegisterRule>)
                      }
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
                  onClick={() =>
                    onChange({ [key]: [...clauses, { findings: [] }] } as Partial<HealthRegisterRule>)
                  }
                >
                  <Plus className="ic" aria-hidden="true" />
                </button>
              ) : null}
            </div>
          );
        })}

        <RowErrors errors={errors} />
      </div>
    </div>
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

/** The verdict a publish would apply, shown while editing rather than only on the button. */
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
  const shown = useMemo(() => (problems ?? []).filter((p) => !p.fatal), [problems]);
  if (shown.length === 0) return null;
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
        <ul className="small muted" style={{ margin: 0, paddingLeft: 18, lineHeight: 1.7 }}>
          {shown.map((p, i) => (
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
    <div className={result.ok ? "alert ok" : "alert"} style={{ marginBottom: 16 }}>
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
    </div>
  );
}
