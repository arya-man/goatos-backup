import { AlertTriangle } from "lucide-react";

import { copy, optionalCopy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  getHealthConfigRegister,
  listHealthConfigRegisters,
  type ApiResult,
  type HealthRegisterDetail,
  type HealthRegisterRow,
} from "@/lib/api/server";

import { RegisterEditor } from "./health-register-editor";
import { StaleVersionNotice } from "./health-stale-version-recovery";
import { RegisterSheetControls, RegisterSheetHeaderControls } from "./health-register-sheet";
import { InfoTooltip } from "@/components/ui-primitives";
import { OpenRegisterDraftButton } from "./health-register-open";

// Health Config -> Diagnosis. The other half of the rulebook: the questions asked about a sick
// animal, the findings each answer produces, and the illnesses those findings point to.
//
// ONE REGISTER PER ANIMAL CLASS, and the classes are not a filter over one table -- they are four
// separate rulebooks. The loudest rule in the clinical contract is that a milk kid is never
// diagnosed against the adult table, so the list is four rows and an author edits one at a time.
//
// EDITS ARE VERSIONED, NEVER IN PLACE, exactly as the treatment protocols beside them are. Every
// diagnosis run pins the register version it was produced under, so a proposal made last month
// stays interpretable after a vet rewrites a rule -- and publishing changes what the NEXT
// observation is judged against, never what an old one meant.

/** A class's live register and, when one is open, its draft — shown together. */
type ClassRow = {
  animalClass: string;
  /** The farm's own name for the type, from the backend. */
  typeLabel?: string;
  live?: HealthRegisterRow;
  draft?: HealthRegisterRow;
};

function groupByClass(rows: HealthRegisterRow[]): ClassRow[] {
  const byClass = new Map<string, ClassRow>();
  for (const row of rows) {
    const entry = byClass.get(row.animal_class) ?? {
      animalClass: row.animal_class,
      typeLabel: row.type_label,
    };
    // Every row of a type carries the same label; the first non-empty one wins so a payload with
    // it on only one row (a type with a draft and no published version) still names itself.
    if (!entry.typeLabel && row.type_label) entry.typeLabel = row.type_label;
    if (row.status === "published") entry.live = row;
    if (row.status === "draft") entry.draft = row;
    byClass.set(row.animal_class, entry);
  }
  return [...byClass.values()].sort((a, b) => a.animalClass.localeCompare(b.animalClass));
}

/**
 * The type as a person says it.
 *
 * The BACKEND'S OWN LABEL WINS, because a type the farm authored has no copy key and deriving one
 * from the machine key put "kid warmup" and "mothers" on a screen a vet reads. The copy key is
 * kept as the fallback for the four shipped classes, whose wording is the product's rather than
 * the farm's, and the machine key is the last resort for a payload from a backend that predates
 * the label.
 */
function className(
  animalClass: string,
  pageContract: AdminUiPageContract,
  typeLabel?: string,
): string {
  if (typeLabel && typeLabel.trim() !== "") return typeLabel;
  const key = `label.class.${animalClass}`;
  return optionalCopy(pageContract, key) ?? animalClass.replace(/_/g, " ");
}

// The three-sentence explainer of what a register IS used to be a card of its own on every view.
// It is now the "i" beside the section title: the words are worth keeping -- "one register per
// animal class" tells a first-time reader nothing until they know what a register does -- but a
// permanent paragraph is rent paid by every later visit for a sentence read once.

function SectionError({
  result,
  pageContract,
}: {
  result: ApiResult<unknown> | null;
  pageContract: AdminUiPageContract;
}) {
  if (!result || result.ok) return null;
  return (
    <div className="alert" style={{ marginBottom: 16 }}>
      <AlertTriangle className="ic" aria-hidden="true" />
      <div>
        <b>{copy(pageContract, "action.error_backend")}</b>
        <div className="small muted">
          {result.error.code ?? result.error.kind}&nbsp;{result.error.message}
        </div>
      </div>
    </div>
  );
}

export async function HealthRegisterSection({
  selectedVersionId,
  pageContract,
  mayWrite,
  writeDisabledReason,
  listHref,
}: {
  selectedVersionId: string;
  pageContract: AdminUiPageContract;
  mayWrite: boolean;
  writeDisabledReason: string;
  listHref: string;
}) {
  // The list and the editor are separate route states, for the same reason the protocol half keeps
  // them apart: reading the catalog behind a full-screen editor is backend fan-out nobody sees.
  const firstListResult = selectedVersionId ? null : await listHealthConfigRegisters();
  const detailResult = selectedVersionId ? await getHealthConfigRegister(selectedVersionId) : null;

  const detail: HealthRegisterDetail | null = detailResult && detailResult.ok ? detailResult.data : null;

  // A selected version that no longer resolves is a REAL state: the author discarded the draft, or
  // a second tab published it, and this tab is left holding an id that points at nothing.
  const selectedVersionIsGone =
    Boolean(selectedVersionId) && detailResult !== null && !detailResult.ok && detailResult.error.kind === "not_found";

  // The dead version recovers TO THE LIST. This branch used to return the notice ALONE, and because
  // the list is deliberately not read behind an open editor, that left "everything below is up to
  // date" sitting over an EMPTY screen -- the state the maintainer hit on 2026-09-23. Publishing is
  // the ordinary way to get here: it retires the draft id the editor URL still holds.
  const listResult = firstListResult ?? (selectedVersionIsGone ? await listHealthConfigRegisters() : null);

  if (detail) {
    return (
      <>
        <RegisterEditor
        detail={detail}
        pageContract={pageContract}
        mayWrite={mayWrite}
        disabledReason={writeDisabledReason}
          listHref={listHref}
        />
      </>
    );
  }

  const rows = listResult?.ok ? groupByClass(listResult.data.registers) : [];
  const cols = tableLabels(pageContract, "register-catalog");

  return (
    <>
      {selectedVersionIsGone ? (
        <StaleVersionNotice
          message={
            optionalCopy(pageContract, "error.stale_version") ?? copy(pageContract, "action.error_backend")
          }
          linkLabel={optionalCopy(pageContract, "action.back_to_list") ?? copy(pageContract, "action.back")}
          listHref={listHref}
        />
      ) : null}
      <SectionError result={listResult} pageContract={pageContract} />
      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <h3>{copy(pageContract, "section.registers.title")}</h3>
          <InfoTooltip label={copy(pageContract, "section.registers.title")}>
            {copy(pageContract, "note.how_it_works")}
          </InfoTooltip>
          <span className="small muted">{copy(pageContract, "section.registers.caption")}</span>
          <div className="sp" style={{ flex: 1 }} />
          {/* The template is one file for every type, and what an upload does is one fact, so
              both live here rather than repeating down the table. */}
          <RegisterSheetHeaderControls pageContract={pageContract} />
        </div>
        <p className="small muted" style={{ margin: "0 14px 10px", lineHeight: 1.6 }}>
          {copy(pageContract, "section.registers.note")}
        </p>
        <div
          className="bd health-scroll"
          style={{ padding: 0, overflowX: "auto" }}
          tabIndex={0}
          role="group"
          aria-label={copy(pageContract, "section.registers.aria")}
        >
          <table className="feed-table" aria-label={copy(pageContract, "section.registers.aria")}>
            <thead>
              <tr>
                {cols.map((col) => (
                  <th key={col}>{col}</th>
                ))}
                <th>{copy(pageContract, "action.edit_register")}</th>
                <th>{copy(pageContract, "action.download_sheet")}</th>
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 ? (
                <tr>
                  <td colSpan={cols.length + 2}>
                    <div
                      className="muted small"
                      style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}
                    >
                      {listResult?.ok
                        ? copy(pageContract, "empty.registers")
                        : copy(pageContract, "action.error_backend")}
                    </div>
                  </td>
                </tr>
              ) : (
                rows.map((row) => {
                  // The DRAFT is what an author opens; the LIVE row is what the herd is being
                  // diagnosed against. Both are shown, because an author needs to see that the
                  // live register still asks something their unpublished draft no longer does.
                  const shown = row.draft ?? row.live;
                  return (
                    <tr key={row.animalClass}>
                      <td>{className(row.animalClass, pageContract, row.typeLabel)}</td>
                      <td className="muted">{shown?.register_label ?? ""}</td>
                      <td>
                        {row.live ? (
                          <span className="tag t-ok">{copy(pageContract, "label.register_live")}</span>
                        ) : (
                          <span className="tag t-warn">{copy(pageContract, "status.no_live")}</span>
                        )}
                        {row.draft ? (
                          <>
                            {" "}
                            <span className="tag t-warn">{copy(pageContract, "label.register_draft")}</span>
                          </>
                        ) : null}
                      </td>
                      <td style={{ fontVariantNumeric: "tabular-nums" }}>{shown?.question_count ?? 0}</td>
                      <td style={{ fontVariantNumeric: "tabular-nums" }}>{shown?.rule_count ?? 0}</td>
                      <td className="muted">
                        {row.live?.published_at ? row.live.published_at.slice(0, 10) : ""}
                      </td>
                      <td>
                        <OpenRegisterDraftButton
                          animalClass={row.animalClass}
                          pageContract={pageContract}
                          enabled={mayWrite}
                          disabledReason={writeDisabledReason}
                          basePath={listHref}
                          // Every ACTIVE type can be opened, including one whose rules nobody has
                          // written: the draft starts empty and names itself. It used to require a
                          // live register to copy from, which left a type created on the Types tab
                          // with no way in at all -- the editor refused, and nothing said why.
                          // An empty document is still fatally unpublishable, so the gate that
                          // stops a blank register reaching animals is untouched.
                          openable
                        />
                      </td>
                      <td>
                        {/* The sheet is offered per TYPE, in the row that names it, so there is
                            never a question of which rulebook a download belongs to. */}
                        <RegisterSheetControls
                          animalClass={row.animalClass}
                          typeLabel={className(row.animalClass, pageContract, row.typeLabel)}
                          pageContract={pageContract}
                          mayWrite={mayWrite}
                          disabledReason={writeDisabledReason}
                        />
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </section>
    </>
  );
}
