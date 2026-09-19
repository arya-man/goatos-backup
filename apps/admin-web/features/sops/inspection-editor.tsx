"use client";

// PROCUREMENT SOP (maintainer decision 2026-09-14, docs/decisions/procurement-sop.md).
//
// The animal purchase inspection editor: PAGES of QUESTIONS the phone runs for every animal in a
// purchase load. The author sets what is asked, on which page and in which order, which kind of
// answer, whether it is compulsory, and which capture (photo / video / either, up to N files).
// Every word on this screen is the backend contract's; the backend validates the document on
// save and refuses one the phone could not run, naming the field.
import { useMemo, useState, useTransition } from "react";
import Link from "@/components/no-prefetch-link";
import { useRouter } from "next/navigation";
import { AlertTriangle, Check, ChevronDown, ChevronLeft, ChevronUp, Lock, Plus, X } from "lucide-react";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  LOCKED_LOAD_KEYS,
  LOCKED_OPTION_KEYS,
  LOCKED_QUESTION_KEYS,
  REQUIRED_LOAD_KEYS,
  VENDOR_LOCKED_KEYS,
  VENDOR_REQUIRED_KEYS,
  blankPage,
  blankQuestion,
  emitInspection,
  emitVendorForm,
  inspectionProblems,
  slugKey,
  vendorFormProblems,
  type CaptureKind,
  type InspectionPageRow,
  type InspectionQuestionRow,
  type InspectionRows,
  type QuestionKind,
} from "./inspection-model";
import { publishInspectionVersion, saveInspectionVersion, type InspectionSaveResult } from "./sop-actions";
import { publishedHref } from "./published-href";
import { followQuestionKey, keyForTitle } from "./weighing-model";

// The editor runs two documents of the same pages-of-questions shape:
//   inspection   the animal purchase inspection (load form + per-animal pages, media allowed);
//   vendor_form  the VENDOR FORM (2026-09-19): what Add / Edit vendor asks -- no load form, no
//                media, the register's columns as locked typed questions whose catalog-backed
//                choices are filled by the backend.
export type InspectionProfile = "inspection" | "vendor_form";

type Props = {
  pageContract: AdminUiPageContract;
  basePath: string;
  sopId: string;
  sopName: string;
  sopCode: string;
  versionLabel: string;
  initial: InspectionRows;
  profile?: InspectionProfile;
};

export function InspectionEditor({ pageContract: pc, basePath, sopId, sopName, sopCode, versionLabel, initial, profile = "inspection" }: Props) {
  const router = useRouter();
  const isVendorForm = profile === "vendor_form";
  const pageLockedKeys = isVendorForm ? VENDOR_LOCKED_KEYS : LOCKED_QUESTION_KEYS;
  const pageRequiredKeys = isVendorForm ? VENDOR_REQUIRED_KEYS : new Set<string>();
  const copyPrefix = isVendorForm ? "vendor_form" : "inspection";
  const [rows, setRows] = useState<InspectionRows>(initial);
  const [openPage, setOpenPage] = useState<string>(initial.pages[0]?.id ?? "");
  // Keys the loaded version already carries never move (locked register questions, stored answers);
  // a new page / question key follows its whole title (keyForTitle).
  const [savedKeys] = useState<Set<string>>(
    () => new Set([...initial.loadForm.map((q) => q.key), ...initial.pages.map((p) => p.key), ...initial.pages.flatMap((p) => p.questions.map((q) => q.key))].filter(Boolean)),
  );
  const [result, setResult] = useState<InspectionSaveResult | null>(null);
  const [pending, startTransition] = useTransition();
  const allKinds = optionGroup(pc, "inspection_question_kinds");
  const kinds = isVendorForm ? allKinds.filter((k) => k.key !== "media") : allKinds;
  const loadKinds = allKinds.filter((k) => k.key !== "media");
  const captures = optionGroup(pc, "inspection_capture_kinds");
  const problems = useMemo(() => (isVendorForm ? vendorFormProblems(rows) : inspectionProblems(rows)), [rows, isVendorForm]);
  const questionCount = rows.pages.reduce((n, p) => n + p.questions.length, 0);
  const takenKeys = useMemo(() => new Set(rows.pages.flatMap((p) => p.questions.map((q) => q.key))), [rows]);

  function updateLoadQuestion(qid: string, patch: Partial<InspectionQuestionRow>) {
    setRows((r) => ({ ...r, loadForm: followQuestionKey(r.loadForm, qid, patch) }));
  }
  function moveLoadQuestion(qid: string, dir: -1 | 1) {
    setRows((r) => {
      const i = r.loadForm.findIndex((q) => q.id === qid);
      const j = i + dir;
      if (i < 0 || j < 0 || j >= r.loadForm.length) return r;
      const next = [...r.loadForm];
      [next[i], next[j]] = [next[j], next[i]];
      return { ...r, loadForm: next };
    });
  }
  function removeLoadQuestion(qid: string) {
    setRows((r) => ({ ...r, loadForm: r.loadForm.filter((q) => q.id !== qid) }));
  }
  function addLoadQuestion() {
    setRows((r) => ({ ...r, loadForm: [...r.loadForm, blankQuestion()] }));
  }
  // A renamed choice value: every question conditioned on (questionKey, oldValue) follows.
  function renameOptionRefs(questionKey: string, from: string, to: string) {
    const follow = (q: InspectionQuestionRow) => (q.onlyIfQuestion === questionKey && q.onlyIfValue === from ? { ...q, onlyIfValue: to } : q);
    setRows((r) => ({ loadForm: r.loadForm.map(follow), pages: r.pages.map((p) => ({ ...p, questions: p.questions.map(follow) })) }));
  }
  function updatePage(id: string, patch: Partial<InspectionPageRow>) {
    setRows((r) => ({ ...r, pages: r.pages.map((p) => (p.id === id ? { ...p, ...patch } : p)) }));
  }
  function updateQuestion(pageId: string, qid: string, patch: Partial<InspectionQuestionRow>) {
    setRows((r) => ({
      ...r,
      // A condition may point at a question on an earlier page, so a moved key is followed across pages.
      pages: (() => {
        const all = followQuestionKey(r.pages.flatMap((p) => p.questions), qid, patch);
        const byId = new Map(all.map((q) => [q.id, q]));
        return r.pages.map((p) => (p.id !== pageId && !p.questions.some((q) => byId.get(q.id) !== q) ? p : { ...p, questions: p.questions.map((q) => byId.get(q.id) ?? q) }));
      })(),
    }));
  }
  function moveQuestion(pageId: string, qid: string, dir: -1 | 1) {
    setRows((r) => ({
      ...r,
      pages: r.pages.map((p) => {
        if (p.id !== pageId) return p;
        const i = p.questions.findIndex((q) => q.id === qid);
        const j = i + dir;
        if (i < 0 || j < 0 || j >= p.questions.length) return p;
        const next = [...p.questions];
        [next[i], next[j]] = [next[j], next[i]];
        return { ...p, questions: next };
      }),
    }));
  }
  function moveQuestionToPage(fromPageId: string, qid: string, toPageId: string) {
    if (fromPageId === toPageId) return;
    setRows((r) => {
      const q = r.pages.find((p) => p.id === fromPageId)?.questions.find((x) => x.id === qid);
      if (!q) return r;
      return {
        ...r,
        pages: r.pages.map((p) => {
          if (p.id === fromPageId) return { ...p, questions: p.questions.filter((x) => x.id !== qid) };
          if (p.id === toPageId) return { ...p, questions: [...p.questions, q] };
          return p;
        }),
      };
    });
  }
  function removeQuestion(pageId: string, qid: string) {
    setRows((r) => ({ ...r, pages: r.pages.map((p) => (p.id !== pageId ? p : { ...p, questions: p.questions.filter((q) => q.id !== qid) })) }));
  }
  function addQuestion(pageId: string) {
    const q = blankQuestion();
    setRows((r) => ({ ...r, pages: r.pages.map((p) => (p.id !== pageId ? p : { ...p, questions: [...p.questions, q] })) }));
  }
  function movePage(id: string, dir: -1 | 1) {
    setRows((r) => {
      const i = r.pages.findIndex((p) => p.id === id);
      const j = i + dir;
      if (i < 0 || j < 0 || j >= r.pages.length) return r;
      const next = [...r.pages];
      [next[i], next[j]] = [next[j], next[i]];
      return { ...r, pages: next };
    });
  }
  function addPage() {
    const p = blankPage();
    setRows((r) => ({ ...r, pages: [...r.pages, p] }));
    setOpenPage(p.id);
  }
  function removePage(id: string) {
    setRows((r) => ({ ...r, pages: r.pages.filter((p) => p.id !== id) }));
  }

  function submit(publish: boolean) {
    const doc = isVendorForm ? emitVendorForm(rows) : emitInspection(rows);
    startTransition(async () => {
      const res = publish ? await publishInspectionVersion(sopId, doc, undefined, profile) : await saveInspectionVersion(sopId, doc, undefined, profile);
      setResult(res);
      if (res.ok && publish) {
        // Publish CLOSES the editor: the library reopens with a banner naming the version and
        // the card it belongs to (maintainer report 2026-09-15).
        router.push(publishedHref(basePath, sopId, res.versionNumber));
        router.refresh();
      }
    });
  }

  return (
    <div className="screen on sop-inspection">
      <div className="phead">
        <div>
          <div className="crumb">
            {copy(pc, "crumb")} · {pc.title} · <b>{sopName}</b>
          </div>
          <h1>{copy(pc, `${copyPrefix}.title`)}</h1>
          <div className="sub">{copy(pc, `${copyPrefix}.subtitle`)}</div>
          <div className="muted small" style={{ marginTop: 4 }}>
            <code>{sopCode}</code> · {versionLabel} · {copy(pc, `${copyPrefix}.notice.capture_kept`)}
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Link className="btn" href={basePath}>
          <ChevronLeft className="ic" /> {copy(pc, "builder.back")}
        </Link>
      </div>

      {result ? (
        <div className={result.ok ? "note" : "alert warn"} style={{ marginBottom: 12 }}>
          {result.ok ? <span className="tag t-ok">{copy(pc, "modal.builder.notice_ok")}</span> : <AlertTriangle className="ic" />} {result.message}
          {result.report && !result.report.valid ? (
            <ul className="small" style={{ margin: "6px 0 0 16px" }}>
              {result.report.errors.slice(0, 6).map((e, i) => (
                <li key={i}>{e.message}</li>
              ))}
            </ul>
          ) : null}
        </div>
      ) : null}

      {isVendorForm ? null : (
      <section className="card inspection-page inspection-loadform">
        <div className="inspection-page-head" style={{ cursor: "default" }}>
          <span className="qnum">L</span>
          <strong>{copy(pc, "inspection.loadform.title")}</strong>
          <span className="muted small">{copy(pc, "inspection.loadform.subtitle")}</span>
        </div>
        <div className="bd">
          <div className="qlist">
            {rows.loadForm.map((q, qi) => (
              <QuestionCard
                key={q.id}
                pc={pc}
                page={null}
                pages={[]}
                index={qi}
                count={rows.loadForm.length}
                q={q}
                kinds={loadKinds}
                captures={captures}
                earlier={rows.loadForm.slice(0, qi)}
                takenKeys={new Set(rows.loadForm.map((x) => x.key))}
                savedKeys={savedKeys}
                lockedKeys={LOCKED_LOAD_KEYS}
                requiredKeys={REQUIRED_LOAD_KEYS}
                onChange={(patch) => updateLoadQuestion(q.id, patch)}
                onOptionRenamed={(from, to) => renameOptionRefs(q.key, from, to)}
                onMove={(dir) => moveLoadQuestion(q.id, dir)}
                onMovePage={() => undefined}
                onRemove={() => removeLoadQuestion(q.id)}
              />
            ))}
            <button type="button" className="btn sm ghost" onClick={addLoadQuestion}>
              <Plus className="ic" /> {copy(pc, "inspection.question.add")}
            </button>
          </div>
        </div>
      </section>
      )}

      <div className="qlist">
        {rows.pages.map((page, pi) => {
          const open = openPage === page.id;
          return (
            <section className="card inspection-page" key={page.id}>
              <button type="button" className="inspection-page-head" aria-expanded={open} onClick={() => setOpenPage(open ? "" : page.id)}>
                <span className="qnum">{pi + 1}</span>
                <strong>{page.title.trim() || `${copy(pc, "inspection.page")} ${pi + 1}`}</strong>
                <span className="muted small">
                  {page.questions.length} {copy(pc, "label.inspection_questions")}
                </span>
                {open ? <ChevronUp className="ic" /> : <ChevronDown className="ic" />}
              </button>
              {open ? (
                <div className="bd">
                  <div className="qcfg">
                    <div className="qcfg-head">
                      <span className="qcfg-title">
                        {copy(pc, "inspection.page")} {pi + 1}
                      </span>
                      <span className="inspection-page-actions">
                        <button type="button" className="ia" aria-label={copy(pc, "inspection.question.move_up")} disabled={pi === 0} onClick={() => movePage(page.id, -1)}>
                          <ChevronUp className="ic" />
                        </button>
                        <button type="button" className="ia" aria-label={copy(pc, "inspection.question.move_down")} disabled={pi === rows.pages.length - 1} onClick={() => movePage(page.id, 1)}>
                          <ChevronDown className="ic" />
                        </button>
                        <button type="button" className="ia del" aria-label={copy(pc, "inspection.page.remove")} disabled={rows.pages.length <= 1} onClick={() => removePage(page.id)}>
                          <X className="ic" />
                        </button>
                      </span>
                    </div>
                    <div className="rowf">
                      <label className="numfield">
                        <span className="numlbl">{copy(pc, "inspection.page.title")}</span>
                        <input
                          value={page.title}
                          placeholder={pi === 0 ? copy(pc, "inspection.page.first_untitled") : ""}
                          onChange={(e) => {
                            const title = e.target.value;
                            updatePage(page.id, { title, key: keyForTitle(title, page.key, savedKeys, new Set(rows.pages.map((p) => p.key)), `page_${pi + 1}`) });
                          }}
                        />
                      </label>
                      <label className="numfield">
                        <span className="numlbl">{copy(pc, "inspection.page.hint")}</span>
                        <input value={page.hint} onChange={(e) => updatePage(page.id, { hint: e.target.value })} />
                      </label>
                    </div>
                    {!page.key ? (
                      <label className="numfield">
                        <span className="numlbl">{copy(pc, "inspection.question.key")}</span>
                        <input value={page.key} onChange={(e) => updatePage(page.id, { key: e.target.value })} placeholder="page_key" />
                      </label>
                    ) : null}
                  </div>
                  <div className="qlist" style={{ marginTop: 10 }}>
                    {page.questions.length === 0 ? <p className="muted">{copy(pc, "inspection.empty")}</p> : null}
                    {page.questions.map((q, qi) => (
                      <QuestionCard
                        key={q.id}
                        pc={pc}
                        page={page}
                        pages={rows.pages}
                        index={qi}
                        count={page.questions.length}
                        q={q}
                        kinds={kinds}
                        captures={captures}
                        earlier={rows.pages.flatMap((p) => p.questions).slice(0, rows.pages.flatMap((p) => p.questions).findIndex((x) => x.id === q.id))}
                        takenKeys={takenKeys}
                        savedKeys={savedKeys}
                        lockedKeys={pageLockedKeys}
                        requiredKeys={pageRequiredKeys}
                        onChange={(patch) => updateQuestion(page.id, q.id, patch)}
                        onOptionRenamed={(from, to) => renameOptionRefs(q.key, from, to)}
                        onMove={(dir) => moveQuestion(page.id, q.id, dir)}
                        onMovePage={(to) => moveQuestionToPage(page.id, q.id, to)}
                        onRemove={() => removeQuestion(page.id, q.id)}
                      />
                    ))}
                    <button type="button" className="btn sm ghost" onClick={() => addQuestion(page.id)}>
                      <Plus className="ic" /> {copy(pc, "inspection.question.add")}
                    </button>
                  </div>
                </div>
              ) : null}
            </section>
          );
        })}
        <button type="button" className="btn sm ghost" onClick={addPage}>
          <Plus className="ic" /> {copy(pc, "inspection.page.add")}
        </button>
      </div>

      <div className="cfgmf inspection-footer">
        <div>
          <strong>{rows.pages.length}</strong> {copy(pc, "label.inspection_pages")} · <strong>{questionCount}</strong> {copy(pc, "label.inspection_questions")}
          {problems.length ? (
            <ul className="small muted" style={{ margin: "4px 0 0 16px" }}>
              {problems.slice(0, 5).map((p, i) => (
                <li key={i}>{p}</li>
              ))}
            </ul>
          ) : null}
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

function QuestionCard({
  pc, page, pages, index, count, q, kinds, captures, earlier, takenKeys, savedKeys, lockedKeys, requiredKeys, onChange, onOptionRenamed, onMove, onMovePage, onRemove,
}: {
  pc: AdminUiPageContract;
  page: InspectionPageRow | null;
  pages: InspectionPageRow[];
  index: number;
  count: number;
  q: InspectionQuestionRow;
  kinds: { key: string; label: string; title?: string }[];
  captures: { key: string; label: string }[];
  earlier: InspectionQuestionRow[];
  takenKeys: Set<string>;
  /** Keys the loaded version carries; any other key follows its title (keyForTitle). */
  savedKeys: Set<string>;
  lockedKeys: Set<string>;
  requiredKeys: Set<string>;
  onChange: (patch: Partial<InspectionQuestionRow>) => void;
  /** A choice's wire value changed: every "ask only when" that pointed at the old value follows. */
  onOptionRenamed: (oldValue: string, newValue: string) => void;
  onMove: (dir: -1 | 1) => void;
  onMovePage: (toPageId: string) => void;
  onRemove: () => void;
}) {
  const locked = lockedKeys.has(q.key);
  // Catalog-backed choices (vendor form) are filled by the backend from the vendor catalog and
  // are not authored here; the card says where they come from instead of listing them.
  const optionsLocked = LOCKED_OPTION_KEYS.has(q.key) || Boolean(q.catalog);
  const requiredLocked = requiredKeys.has(q.key);
  const isVendor = q.kind === "vendor";
  const dep = earlier.find((e) => e.key === q.onlyIfQuestion);
  return (
    <div className="qcard">
      <div className="qhead">
        <span className="qnum">{index + 1}</span>
        <span className="qtype">
          <select
            value={q.kind}
            disabled={locked}
            onChange={(e) => {
              if (isVendor) return;
              const kind = e.target.value as QuestionKind;
              onChange({
                kind,
                options: (kind === "choice" || kind === "multi") && q.options.length === 0
                  ? [{ value: "yes", label: copy(pc, "option.yes") }, { value: "no", label: copy(pc, "option.no") }]
                  : q.options,
                maxFiles: kind === "media" && q.maxFiles === 0 ? 1 : q.maxFiles,
              });
            }}
          >
            {isVendor ? <option value="vendor">{copy(pc, "inspection.kind.vendor")}</option> : null}
            {kinds.map((k) => (
              <option key={k.key} value={k.key} title={k.title}>
                {k.label}
              </option>
            ))}
          </select>
        </span>
        {locked ? (
          <span className="muted small" title={copy(pc, "inspection.notice.locked")}>
            <Lock className="ic" style={{ width: 12 }} /> {q.key}
          </span>
        ) : null}
        <span className="sp" style={{ flex: 1 }} />
        <button type="button" className="ia" aria-label={copy(pc, "inspection.question.move_up")} disabled={index === 0} onClick={() => onMove(-1)}>
          <ChevronUp className="ic" />
        </button>
        <button type="button" className="ia" aria-label={copy(pc, "inspection.question.move_down")} disabled={index === count - 1} onClick={() => onMove(1)}>
          <ChevronDown className="ic" />
        </button>
        <button type="button" className="ia del" aria-label={copy(pc, "inspection.question.remove")} disabled={locked} onClick={onRemove}>
          <X className="ic" />
        </button>
      </div>
      <div className="qbody">
        <label className="numfield">
          <span className="numlbl">{copy(pc, "inspection.question.title")}</span>
          <input
            className="qtext"
            value={q.title}
            onChange={(e) => {
              const title = e.target.value;
              onChange({ title, key: keyForTitle(title, q.key, savedKeys, takenKeys) });
            }}
          />
        </label>
        <label className="numfield">
          <span className="numlbl">{copy(pc, "inspection.question.hint")}</span>
          <textarea className="qhelp" rows={2} value={q.hint} onChange={(e) => onChange({ hint: e.target.value })} />
        </label>
        {!q.key && !locked ? (
          <label className="numfield">
            <span className="numlbl">{copy(pc, "inspection.question.key")}</span>
            <input value={q.key} onChange={(e) => onChange({ key: e.target.value })} />
          </label>
        ) : null}

        {q.catalog ? (
          <div className="qcfg">
            <span className="muted small">
              <Lock className="ic" style={{ width: 12 }} /> {copy(pc, "vendor_form.notice.catalog_choices")} <code>{q.catalog}</code>
            </span>
          </div>
        ) : null}
        {(q.kind === "choice" || q.kind === "multi") && !q.catalog ? (
          <div className="qcfg">
            <div className="qcfg-head">
              <span className="qcfg-title">{copy(pc, "inspection.question.options")}</span>
              {!optionsLocked ? (
                <button type="button" className="btn sm ghost" onClick={() => onChange({ options: [...q.options, { value: "", label: "" }] })}>
                  <Plus className="ic" /> {copy(pc, "inspection.question.add_option")}
                </button>
              ) : null}
            </div>
            {q.options.map((o, i) => (
              <div className="optrow" key={i}>
                <span className="optmark">{q.kind === "choice" ? <span className="optdot" /> : <span className="optbox" />}</span>
                <input
                  value={o.label}
                  disabled={optionsLocked}
                  onChange={(e) => {
                    const label = e.target.value;
                    // The wire value follows the label (unique within the question), so a renamed
                    // Yes/No default becomes truck/tractor, never "yes" wearing a Truck label. The
                    // "other" value is kept: it is what attaches the free text.
                    const others = new Set(q.options.filter((_, j) => j !== i).map((y) => y.value));
                    const value = o.value === "other" ? "other" : slugKey(label, others, "choice");
                    const options = q.options.map((x, j) => (j === i ? { label, value } : x));
                    onChange({ options });
                    if (o.value && o.value !== value) onOptionRenamed(o.value, value);
                  }}
                />
                <code className="muted small">{o.value}</code>
                {!optionsLocked ? (
                  <button type="button" className="ia del" aria-label={copy(pc, "inspection.question.remove")} onClick={() => onChange({ options: q.options.filter((_, j) => j !== i) })}>
                    <X className="ic" />
                  </button>
                ) : null}
              </div>
            ))}
            {!optionsLocked ? (
              <label className="chkline">
                <input type="checkbox" checked={q.allowOther} onChange={(e) => onChange({ allowOther: e.target.checked })} /> {copy(pc, "inspection.question.allow_other")}
              </label>
            ) : null}
          </div>
        ) : null}

        {q.kind === "media" ? (
          <div className="qcfg">
            <div className="rowf">
              <label className="numfield">
                <span className="numlbl">{copy(pc, "inspection.question.accepts")}</span>
                <select value={q.accepts} onChange={(e) => onChange({ accepts: e.target.value as CaptureKind })}>
                  {captures.map((c) => (
                    <option key={c.key} value={c.key}>
                      {c.label}
                    </option>
                  ))}
                </select>
              </label>
              <label className="numfield">
                <span className="numlbl">{copy(pc, "inspection.question.max_files")}</span>
                <input type="number" min={1} max={10} value={q.maxFiles} onChange={(e) => onChange({ maxFiles: Math.max(1, Math.min(10, Number(e.target.value) || 1)) })} />
              </label>
            </div>
          </div>
        ) : null}

        {q.kind === "number" ? (
          <div className="qcfg">
            <div className="rowf">
              <label className="numfield">
                <span className="numlbl">{copy(pc, "inspection.question.min")}</span>
                <input value={q.min} inputMode="decimal" onChange={(e) => onChange({ min: e.target.value })} />
              </label>
              <label className="numfield">
                <span className="numlbl">{copy(pc, "inspection.question.max")}</span>
                <input value={q.max} inputMode="decimal" onChange={(e) => onChange({ max: e.target.value })} />
              </label>
              <label className="numfield">
                <span className="numlbl">{copy(pc, "inspection.question.unit")}</span>
                <input value={q.unit} onChange={(e) => onChange({ unit: e.target.value })} />
              </label>
            </div>
          </div>
        ) : null}

        <div className="qfoot">
          <label className="chkline">
            <input type="checkbox" checked={q.required} disabled={requiredLocked} onChange={(e) => onChange({ required: e.target.checked })} /> {copy(pc, "inspection.question.required")}
          </label>
          <span className="condrow">
            {copy(pc, "inspection.question.only_if")}
            <select value={q.onlyIfQuestion} onChange={(e) => onChange({ onlyIfQuestion: e.target.value, onlyIfValue: "" })}>
              <option value="">{copy(pc, "inspection.question.always")}</option>
              {earlier.filter((e) => e.kind === "choice" && !e.catalog).map((e) => (
                <option key={e.key} value={e.key}>
                  {e.title || e.key}
                </option>
              ))}
            </select>
            {dep ? (
              <>
                {copy(pc, "inspection.question.only_if_value")}
                <select className="condval" value={q.onlyIfValue} onChange={(e) => onChange({ onlyIfValue: e.target.value })}>
                  <option value="">—</option>
                  {dep.options.map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </select>
              </>
            ) : null}
          </span>
          {page && pages.length > 1 ? (
            <span className="condrow">
              {copy(pc, "inspection.question.move_page")}
              <select value={page.id} onChange={(e) => onMovePage(e.target.value)}>
                {pages.map((p, i) => (
                  <option key={p.id} value={p.id}>
                    {p.title.trim() || `${copy(pc, "inspection.page")} ${i + 1}`}
                  </option>
                ))}
              </select>
            </span>
          ) : null}
        </div>
      </div>
    </div>
  );
}
