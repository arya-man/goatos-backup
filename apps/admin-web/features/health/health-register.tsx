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
  live?: HealthRegisterRow;
  draft?: HealthRegisterRow;
};

function groupByClass(rows: HealthRegisterRow[]): ClassRow[] {
  const byClass = new Map<string, ClassRow>();
  for (const row of rows) {
    const entry = byClass.get(row.animal_class) ?? { animalClass: row.animal_class };
    if (row.status === "published") entry.live = row;
    if (row.status === "draft") entry.draft = row;
    byClass.set(row.animal_class, entry);
  }
  return [...byClass.values()].sort((a, b) => a.animalClass.localeCompare(b.animalClass));
}

/**
 * The class as a person says it. The stored value is a machine key (`kid_milk`); a farm reads
 * "Kids on milk", and the phrase is what a vet recognises from the shed rather than from a schema.
 */
function className(animalClass: string, pageContract: AdminUiPageContract): string {
  const key = `label.class.${animalClass}`;
  return optionalCopy(pageContract, key) ?? animalClass.replace(/_/g, " ");
}

/**
 * What a register IS, in three sentences, on every view of it.
 *
 * It sits on the LIST as well as the editor because that is where someone opening this
 * screen for the first time lands, and "one register per animal class" tells them
 * nothing until they know what a register does.
 */
function HowItWorks({ pageContract }: { pageContract: AdminUiPageContract }) {
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="bd">
        <p className="small muted" style={{ margin: 0, lineHeight: 1.7 }}>
          {copy(pageContract, "note.how_it_works")}
        </p>
      </div>
    </section>
  );
}

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
        <HowItWorks pageContract={pageContract} />
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
      <HowItWorks pageContract={pageContract} />
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
          <span className="small muted">{copy(pageContract, "section.registers.caption")}</span>
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
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 ? (
                <tr>
                  <td colSpan={cols.length + 1}>
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
                      <td>{className(row.animalClass, pageContract)}</td>
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
                          // A class with no live register has nothing to copy a draft from, so the
                          // editor cannot be opened on it. The seed publishes version 1; until it
                          // has run there is nothing to edit, and saying so beats a button that
                          // fails when pressed.
                          openable={Boolean(row.live || row.draft)}
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
