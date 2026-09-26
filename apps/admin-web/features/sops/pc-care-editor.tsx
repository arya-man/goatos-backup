"use client";
import Box from "@mui/material/Box";

// PC CARE SOP (maintainer decision 2026-09-22, docs/decisions/pc-care-sop.md).
//
// The preventive-care cards editor: the FEED & WATER REMOVAL rules (whether the evening-before
// removal applies, to which work, from which evening, and what the crew records and answers per
// pen) and one CARD PER WORK CATEGORY -- the instruction, the captures the operator records for
// every animal, and the questions answered once per task when it is submitted.
//
// Every word on this screen is the backend contract's; the backend validates the document on save
// and refuses one the phone could not render, naming the field. The module's locks -- how the
// operator reaches an animal (scan or pen roster), the free-flow scan, the whole-task submit, one
// verifier item per task, and who may plan -- are shown, not edited.
import { useMemo, useState, useTransition, type InputHTMLAttributes } from "react";
import { useRouter } from "next/navigation";
import { AlertTriangle, Check, Lock, Plus } from "lucide-react";
import MuiButton from "@mui/material/Button";
import MuiTextField from "@mui/material/TextField";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { blankProofSlot, blankQuestion, type RemovalProofRow, type WeighingQuestionRow } from "./weighing-model";
import { QuestionCard } from "./weighing-editor";
import { SlotCard } from "./feed-editor";
import {
  PC_CARE_CATEGORIES,
  blankCapture,
  emitPcCare,
  pcCareProblems,
  type PcCareCaptureRow,
  type PcCareCategory,
  type PcCareCategoryRows,
  type PcCareRemovalMode,
  type PcCareRows,
} from "./pc-care-model";
import { FieldSelect, EditorHeader, inspectionEditorSx } from "./editor-chrome";
import { publishedHref } from "./published-href";
import { PcCareFlow, type PcCareInsert, type PcCareRef, type PcCareSection } from "./pc-care-flow";
import { publishPcCareVersion, savePcCareVersion, type PcCareSaveResult } from "./sop-actions";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";
import Alert from "@mui/material/Alert";
import { SegmentTabs } from "@/components/minimal/list/segment-tabs";

type Props = {
  pageContract: AdminUiPageContract;
  basePath: string;
  sopId: string;
  sopName: string;
  sopCode: string;
  versionLabel: string;
  initial: PcCareRows;
  /** The view the page opened on (`?view=flow`), read on the server so SSR and client agree. */
  initialView?: "list" | "flow";
};

export function PcCareEditor({ pageContract: pc, basePath, sopId, sopName, versionLabel, initial, initialView = "list" }: Props) {
  const router = useRouter();
  const [rows, setRows] = useState<PcCareRows>(initial);
  // Keys the loaded version already carries never move; a new capture / question follows its title.
  const [savedKeys] = useState<Set<string>>(
    () =>
      new Set(
        [
          ...initial.removal.proofs.map((p) => p.key),
          ...initial.removal.questions.map((q) => q.key),
          ...PC_CARE_CATEGORIES.flatMap((c) => [...initial.categories[c].proofs.map((p) => p.key), ...initial.categories[c].questions.map((q) => q.key)]),
        ].filter(Boolean),
      ),
  );
  const [result, setResult] = useState<PcCareSaveResult | null>(null);
  const [pending, startTransition] = useTransition();
  const kinds = optionGroup(pc, "wsop_question_kinds");
  const proofKinds = optionGroup(pc, "wsop_proof_kinds");
  const removalModes = optionGroup(pc, "pcsop_removal_modes");
  const categoryOptions = optionGroup(pc, "pcsop_categories");
  const categoryLabel = (category: PcCareCategory) => categoryOptions.find((o) => o.key === category)?.label ?? category;
  const removalLabel = copy(pc, "pcsop.section.removal");
  const problems = useMemo(() => pcCareProblems(rows, categoryLabel, removalLabel), [rows]); // eslint-disable-line react-hooks/exhaustive-deps

  function patchCategory(category: PcCareCategory, fn: (c: PcCareCategoryRows) => PcCareCategoryRows) {
    setRows((r) => ({ ...r, categories: { ...r.categories, [category]: fn(r.categories[category]) } }));
  }
  function patchRemoval(fn: (r: PcCareRows["removal"]) => PcCareRows["removal"]) {
    setRows((r) => ({ ...r, removal: fn(r.removal) }));
  }
  // One accessor pair so the chart and the list edit the same rows whichever section they name.
  function sectionRows(section: PcCareSection) {
    return section === "removal" ? rows.removal : rows.categories[section as PcCareCategory];
  }
  function patchSection(section: PcCareSection, fn: (s: { proofs: RemovalProofRow[]; questions: WeighingQuestionRow[] }) => { proofs?: RemovalProofRow[]; questions?: WeighingQuestionRow[] }) {
    if (section === "removal") patchRemoval((r) => ({ ...r, ...fn(r) }));
    else patchCategory(section as PcCareCategory, (c) => ({ ...c, ...(fn(c) as Partial<PcCareCategoryRows>) }));
  }

  // LIST (default) / FLOW (the chart of the work). Both views edit the same rows.
  const [view, setView] = useState<"list" | "flow">(initialView);
  const [selectedRef, setSelectedRef] = useState<PcCareRef | null>(null);
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
  function insertAt(insert: PcCareInsert) {
    if (insert.kind === "proof") {
      const row = insert.section === "removal" ? blankProofSlot() : blankCapture();
      patchSection(insert.section, (s) => {
        const proofs = [...s.proofs];
        proofs.splice(Math.min(Math.max(insert.index, 0), proofs.length), 0, row);
        return { proofs };
      });
      setSelectedRef({ section: insert.section, kind: "proof", id: row.id });
      return;
    }
    const row = blankQuestion();
    patchSection(insert.section, (s) => {
      const questions = [...s.questions];
      questions.splice(Math.min(Math.max(insert.index, 0), questions.length), 0, row);
      return { questions };
    });
    setSelectedRef({ section: insert.section, kind: "question", id: row.id });
  }

  function slotCardFor(section: PcCareSection, p: RemovalProofRow, i: number) {
    const block = sectionRows(section);
    const capture = section === "removal" ? null : (p as PcCareCaptureRow);
    return (
      <div key={p.id}>
        <SlotCard
          kindLabel={copy(pc, "pcsop.flow.capture")}
          pc={pc}
          index={i}
          count={block.proofs.length}
          slot={p}
          proofKinds={proofKinds}
          takenKeys={new Set(block.proofs.map((x) => x.key))}
          savedKeys={savedKeys}
          onChange={(patch) => patchSection(section, (s) => ({ proofs: s.proofs.map((x) => (x.id === p.id ? { ...x, ...patch } : x)) }))}
          onMove={(dir) =>
            patchSection(section, (s) => {
              const idx = s.proofs.findIndex((x) => x.id === p.id);
              const j = idx + dir;
              if (idx < 0 || j < 0 || j >= s.proofs.length) return { proofs: s.proofs };
              const next = [...s.proofs];
              [next[idx], next[j]] = [next[j], next[idx]];
              return { proofs: next };
            })
          }
          onRemove={() => {
            patchSection(section, (s) => ({ proofs: s.proofs.filter((x) => x.id !== p.id) }));
            setSelectedRef(null);
          }}
        />
        {capture ? (
          <div className="qcfg pcsop-capture-seconds">
            <label className="numfield">
              <span className="numlbl">{copy(pc, "pcsop.capture.min_seconds")}</span>
              <MuiTextField
                fullWidth
                size="small"
                type="number"
                slotProps={{ htmlInput: { min: 0, max: 600 } }}
                value={capture.minSeconds}
                disabled={capture.kind === "photo"}
                onChange={(e) => patchSection(section, (s) => ({ proofs: s.proofs.map((x) => (x.id === p.id ? ({ ...x, minSeconds: e.target.value } as RemovalProofRow) : x)) }))}
              />
            </label>
            <span className="muted small">{capture.kind === "photo" ? copy(pc, "pcsop.capture.min_seconds.photo") : copy(pc, "pcsop.capture.min_seconds.hint")}</span>
          </div>
        ) : null}
      </div>
    );
  }

  function questionCardFor(section: PcCareSection, q: WeighingQuestionRow, qi: number) {
    const block = sectionRows(section);
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
          patchSection(section, (s) => ({
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
          patchSection(section, (s) => ({
            questions: s.questions.map((x) => (x.onlyIfQuestion === q.key && x.onlyIfValue === from ? { ...x, onlyIfValue: to } : x)),
          }))
        }
        onMove={(dir: -1 | 1) =>
          patchSection(section, (s) => {
            const idx = s.questions.findIndex((x) => x.id === q.id);
            const j = idx + dir;
            if (idx < 0 || j < 0 || j >= s.questions.length) return { questions: s.questions };
            const next = [...s.questions];
            [next[idx], next[j]] = [next[j], next[idx]];
            return { questions: next };
          })
        }
        onRemove={() => {
          patchSection(section, (s) => ({ questions: s.questions.filter((x) => x.id !== q.id) }));
          setSelectedRef(null);
        }}
      />
    );
  }

  function cardFor(ref: PcCareRef) {
    const block = sectionRows(ref.section);
    if (ref.kind === "proof") {
      const i = block.proofs.findIndex((p) => p.id === ref.id);
      return i < 0 ? null : <div className="qlist">{slotCardFor(ref.section, block.proofs[i], i)}</div>;
    }
    const qi = block.questions.findIndex((q) => q.id === ref.id);
    return qi < 0 ? null : <div className="qlist">{questionCardFor(ref.section, block.questions[qi], qi)}</div>;
  }

  function submit(publish: boolean) {
    const doc = emitPcCare(rows);
    startTransition(async () => {
      const res = publish ? await publishPcCareVersion(sopId, doc) : await savePcCareVersion(sopId, doc);
      setResult(res);
      if (res.ok && publish) {
        router.push(publishedHref(basePath, sopId, res.versionNumber));
        router.refresh();
      }
    });
  }

  const removalOff = rows.removal.mode === "off";

  return (
    <Box className="screen on sop-inspection sop-weighing sop-pc-care" sx={inspectionEditorSx}>
      <EditorHeader
        crumbs={[copy(pc, "crumb"), pc.title, sopName]}
        title={copy(pc, "pcsop.title")}
        subtitle={copy(pc, "pcsop.subtitle")}
        version={versionLabel}
        notice={copy(pc, "pcsop.notice.pinned")}
        backHref={basePath}
        actions={
          <SegmentTabs
            className="studio-view-toggle"
            ariaLabel={copy(pc, "studio.view.label")}
            value={view}
            tabs={[
              { value: "list", label: copy(pc, "studio.view.list"), onClick: () => switchView("list"), testId: "studio-view-list" },
              { value: "flow", label: copy(pc, "studio.view.flow"), onClick: () => switchView("flow"), testId: "studio-view-flow" },
            ]}
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
        <PcCareFlow pc={pc} rows={rows} categoryLabel={categoryLabel} proofKindLabels={proofKindLabels} selected={selectedRef} onSelect={setSelectedRef} onInsert={insertAt} renderCard={cardFor} />
      ) : null}

      {view === "flow" ? null : (
        <>
          <section className="card inspection-page" data-testid="pcsop-removal">
            <div className="inspection-page-head" style={{ cursor: "default" }}>
              <span className="qnum">1</span>
              <strong>{copy(pc, "pcsop.section.removal")}</strong>
              <span className="muted small">{copy(pc, "pcsop.section.removal.subtitle")}</span>
            </div>
            <div className="bd">
              <div className="qcfg">
                {/* The kit select, like every other field in the studio: the frame admits no
                    native <select>. It reports the chosen value exactly as event.target.value
                    did, so the apply logic below is unchanged. */}
                <FieldSelect
                  label={copy(pc, "pcsop.removal.mode")}
                  value={rows.removal.mode}
                  options={removalModes.map((m) => ({ value: m.key, label: m.label }))}
                  onChange={(value) => patchRemoval((r) => ({ ...r, mode: value as PcCareRemovalMode }))}
                />
              </div>

              {removalOff ? (
                <p className="muted small" style={{ margin: "10px 0 0" }}>
                  <Lock className="ic" style={{ width: 12 }} /> {copy(pc, "pcsop.removal.off_note")}
                </p>
              ) : (
                <>
                  <div className="qcfg" style={{ marginTop: 10 }}>
                    <div className="qcfg-head">
                      <span className="qcfg-title">{copy(pc, "pcsop.removal.applies_to")}</span>
                      <span className="muted small">{copy(pc, "pcsop.removal.applies_to.subtitle")}</span>
                    </div>
                    <div className="qlist">
                      {PC_CARE_CATEGORIES.map((category) => (
                        <FormControlLabel key={category} className="chkline" control={<Checkbox checked={rows.removal.appliesTo.includes(category)} onChange={(e) =>
                              patchRemoval((r) => ({
                                ...r,
                                appliesTo: e.target.checked ? [...r.appliesTo, category].filter((c, i, a) => a.indexOf(c) === i) : r.appliesTo.filter((c) => c !== category),
                              }))
                            } sx={{ p: { xs: 1.5, sm: 1 } }} slotProps={{ input: { "data-testid": `pcsop-applies-${category}` } as InputHTMLAttributes<HTMLInputElement> }} />} label={<>{" "}
                          {categoryLabel(category)}</>} />
                      ))}
                      {rows.removal.appliesTo.length === 0 ? <p className="muted small">{copy(pc, "pcsop.removal.applies_to.empty")}</p> : null}
                    </div>
                  </div>

                  <div className="qcfg" style={{ marginTop: 10 }}>
                    <label className="numfield">
                      <span className="numlbl">{copy(pc, "pcsop.removal.cutoff")}</span>
                      <MuiTextField fullWidth size="small" value={rows.removal.cutoffTime} placeholder={copy(pc, "pcsop.removal.cutoff.farm")} onChange={(e) => patchRemoval((r) => ({ ...r, cutoffTime: e.target.value }))} />
                    </label>
                    <span className="muted small">{copy(pc, "pcsop.removal.cutoff.hint")}</span>
                  </div>

                  <div className="qcfg" style={{ marginTop: 10 }}>
                    <label className="numfield">
                      <span className="numlbl">{copy(pc, "pcsop.removal.instruction")}</span>
                      <textarea className="qhelp" rows={3} value={rows.removal.instruction} onChange={(e) => patchRemoval((r) => ({ ...r, instruction: e.target.value }))} />
                    </label>
                  </div>

                  <div className="qcfg" style={{ marginTop: 10 }}>
                    <div className="qcfg-head">
                      <span className="qcfg-title">{copy(pc, "pcsop.removal.proofs")}</span>
                      <span className="muted small">{copy(pc, "pcsop.removal.proofs.subtitle")}</span>
                    </div>
                    <div className="qlist">
                      {rows.removal.proofs.map((p, i) => slotCardFor("removal", p, i))}
                      <MuiButton type="button" variant="text" size="small" startIcon={<Plus className="ic" />} onClick={() => patchRemoval((r) => ({ ...r, proofs: [...r.proofs, blankProofSlot()] }))}>
                        {copy(pc, "pcsop.category.add_capture")}
                      </MuiButton>
                    </div>
                  </div>

                  <div className="qcfg" style={{ marginTop: 10 }}>
                    <div className="qcfg-head">
                      <span className="qcfg-title">{copy(pc, "pcsop.removal.questions")}</span>
                      <span className="muted small">{copy(pc, "pcsop.removal.questions.subtitle")}</span>
                    </div>
                    <div className="qlist">
                      {rows.removal.questions.length === 0 ? <p className="muted">{copy(pc, "pcsop.removal.questions.empty")}</p> : null}
                      {rows.removal.questions.map((q, qi) => questionCardFor("removal", q, qi))}
                      <MuiButton type="button" variant="text" size="small" startIcon={<Plus className="ic" />} onClick={() => patchRemoval((r) => ({ ...r, questions: [...r.questions, blankQuestion()] }))}>
                        {copy(pc, "inspection.question.add")}
                      </MuiButton>
                    </div>
                  </div>
                </>
              )}
            </div>
          </section>

          <section className="card inspection-page">
            <div className="inspection-page-head" style={{ cursor: "default" }}>
              <span className="qnum">2</span>
              <strong>{copy(pc, "pcsop.section.categories")}</strong>
              <span className="muted small">{copy(pc, "pcsop.section.categories.subtitle")}</span>
            </div>
          </section>

          {PC_CARE_CATEGORIES.map((category, ci) => {
            const block = rows.categories[category];
            return (
              <section className="card inspection-page" key={category} data-testid={`pcsop-category-${category}`}>
                <div className="inspection-page-head" style={{ cursor: "default" }}>
                  <span className="qnum">{ci + 1}</span>
                  <strong>{categoryLabel(category)}</strong>
                  <span className="muted small">{category === "hoof_trimming" || category === "hair_trimming" ? copy(pc, "pcsop.flow.reach_roster") : copy(pc, "pcsop.flow.reach_scan")}</span>
                </div>
                <div className="bd">
                  <div className="qcfg">
                    <label className="numfield">
                      <span className="numlbl">{copy(pc, "pcsop.category.instruction")}</span>
                      <textarea className="qhelp" rows={2} value={block.instruction} onChange={(e) => patchCategory(category, (c) => ({ ...c, instruction: e.target.value }))} />
                    </label>
                  </div>

                  <div className="qcfg" style={{ marginTop: 10 }}>
                    <div className="qcfg-head">
                      <span className="qcfg-title">{copy(pc, "pcsop.category.proofs")}</span>
                      <span className="muted small">{copy(pc, "pcsop.category.proofs.subtitle")}</span>
                    </div>
                    <div className="qlist">
                      {block.proofs.map((p, i) => slotCardFor(category, p, i))}
                      <MuiButton type="button" variant="text" size="small" startIcon={<Plus className="ic" />} onClick={() => patchCategory(category, (c) => ({ ...c, proofs: [...c.proofs, blankCapture()] }))} data-testid={`pcsop-add-capture-${category}`}>
                        {copy(pc, "pcsop.category.add_capture")}
                      </MuiButton>
                    </div>
                  </div>

                  <div className="qcfg" style={{ marginTop: 10 }}>
                    <div className="qcfg-head">
                      <span className="qcfg-title">{copy(pc, "pcsop.category.questions")}</span>
                      <span className="muted small">{copy(pc, "pcsop.category.questions.subtitle")}</span>
                    </div>
                    <div className="qlist">
                      {block.questions.length === 0 ? <p className="muted">{copy(pc, "pcsop.category.questions.empty")}</p> : null}
                      {block.questions.map((q, qi) => questionCardFor(category, q, qi))}
                      <MuiButton type="button" variant="text" size="small" startIcon={<Plus className="ic" />} onClick={() => patchCategory(category, (c) => ({ ...c, questions: [...c.questions, blankQuestion()] }))}>
                        {copy(pc, "inspection.question.add")}
                      </MuiButton>
                    </div>
                  </div>
                </div>
              </section>
            );
          })}
        </>
      )}

      <div className="cfgmf inspection-footer">
        <div>
          {problems.length ? (
            <ul className="small muted" style={{ margin: "4px 0 0 16px" }}>
              {problems.slice(0, 5).map((p, i) => (
                <li key={i}>{p}</li>
              ))}
            </ul>
          ) : (
            <span className="muted small">{copy(pc, "pcsop.footer.ready")}</span>
          )}
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <MuiButton type="button" variant="outlined" disabled={pending || problems.length > 0} onClick={() => submit(false)}>
          {copy(pc, "inspection.action.save_draft")}
        </MuiButton>
        <MuiButton type="button" variant="contained" color="primary" startIcon={<Check className="ic" />} disabled={pending || problems.length > 0} onClick={() => submit(true)}>
          {copy(pc, "inspection.action.publish")}
        </MuiButton>
      </div>
    </Box>
  );
}
