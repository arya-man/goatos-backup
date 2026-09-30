import Table from "@mui/material/Table";
import { InfoTip } from "@/components/app/info-tip";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import { copy, optionalCopy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { Caption } from "@/components/app/caption";
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
import { Tag } from "@/components/ui-primitives";
import { OpenRegisterDraftButton } from "./health-register-open";
import Alert from "@mui/material/Alert";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom } from "@/components/app/table";

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
    <Alert severity="error" sx={{ mb: 2 }}><div>
        <Typography variant="subtitle2" component="div">{copy(pageContract, "action.error_backend")}</Typography>
        <Typography variant="body2" component="div" sx={{ color: "text.secondary" }}>
          {result.error.code ?? result.error.kind}&nbsp;{result.error.message}
        </Typography>
      </div>
    </Alert>
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
      <Card component="section" sx={{ mb: 2 }}>
        <CardHeader
          title={
            <Stack direction="row" spacing={0.5} sx={{ alignItems: "center", flexWrap: "wrap" }}>
              <span>{copy(pageContract, "section.registers.title")}</span>
              <InfoTip title={copy(pageContract, "note.how_it_works")} />
            </Stack>
          }
          subheader={copy(pageContract, "section.registers.caption")}
          // The template is one file for every type, and what an upload does is one fact, so both
          // live here rather than repeating down the table.
          action={<RegisterSheetHeaderControls pageContract={pageContract} />}
          sx={{ mb: 1, flexWrap: "wrap", gap: 1 }}
        />
        <Caption>{copy(pageContract, "section.registers.note")}</Caption>
        <Scrollbar tabIndex={0} role="group" aria-label={copy(pageContract, "section.registers.aria")}>
          <Table aria-label={copy(pageContract, "section.registers.aria")} sx={{ minWidth: 720 }}>
            <TableHeadCustom
              headCells={[
                ...cols.map((col, index) => ({ id: `c${index}`, label: col })),
                { id: "edit", label: copy(pageContract, "action.edit_register") },
                { id: "sheet", label: copy(pageContract, "action.download_sheet") },
              ]}
            />
            <TableBody>
              {rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={cols.length + 2}>
                    <Typography
                      variant="body2"
                      component="div"
                      sx={{ color: "text.secondary", py: 2, px: 0.5, textAlign: "center" }}
                    >
                      {listResult?.ok
                        ? copy(pageContract, "empty.registers")
                        : copy(pageContract, "action.error_backend")}
                    </Typography>
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((row) => {
                  // The DRAFT is what an author opens; the LIVE row is what the herd is being
                  // diagnosed against. Both are shown, because an author needs to see that the
                  // live register still asks something their unpublished draft no longer does.
                  const shown = row.draft ?? row.live;
                  return (
                    <TableRow key={row.animalClass}>
                      <TableCell>{className(row.animalClass, pageContract, row.typeLabel)}</TableCell>
                      <TableCell sx={{ color: "text.secondary" }}>{shown?.register_label ?? ""}</TableCell>
                      <TableCell>
                        {row.live ? (
                          <Tag tone="ok">{copy(pageContract, "label.register_live")}</Tag>
                        ) : (
                          <Tag tone="warn">{copy(pageContract, "status.no_live")}</Tag>
                        )}
                        {row.draft ? (
                          <>
                            {" "}
                            <Tag tone="warn">{copy(pageContract, "label.register_draft")}</Tag>
                          </>
                        ) : null}
                      </TableCell>
                      <TableCell sx={{ fontVariantNumeric: "tabular-nums" }}>{shown?.question_count ?? 0}</TableCell>
                      <TableCell sx={{ fontVariantNumeric: "tabular-nums" }}>{shown?.rule_count ?? 0}</TableCell>
                      <TableCell sx={{ color: "text.secondary" }}>
                        {row.live?.published_at ? row.live.published_at.slice(0, 10) : ""}
                      </TableCell>
                      <TableCell>
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
                      </TableCell>
                      <TableCell>
                        {/* The sheet is offered per TYPE, in the row that names it, so there is
                            never a question of which rulebook a download belongs to. */}
                        <RegisterSheetControls
                          animalClass={row.animalClass}
                          typeLabel={className(row.animalClass, pageContract, row.typeLabel)}
                          pageContract={pageContract}
                          mayWrite={mayWrite}
                          disabledReason={writeDisabledReason}
                        />
                      </TableCell>
                    </TableRow>
                  );
                })
              )}
            </TableBody>
          </Table>
        </Scrollbar>
      </Card>
    </>
  );
}
