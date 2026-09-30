"use client";
import { HiddenField } from "@/components/app/hidden-field";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

// FARM VALUATION section on Sales Config (maintainer instruction 2026-09-19; the stage list became
// the farm's own on 2026-09-24). The herd is valued in the stages written here: what each is
// called, which entries of the farm's herd register it covers, and what a female and a male in it
// are carried at. One form, one save, landing in place.
//
// The stage the valuation could not price is the reason this screen exists. On the day it was
// built 58 kids stood in Warmup -- a stage the register has always carried -- valued at nothing,
// because six stages were written into a query. So the register is shown here with its live head
// counts and the ones nothing values are offered to add in one click: the screen answers "which of
// my animals is nothing pricing" before the farm has to ask it.
//
// Every sentence is backend copy.
import { useActionState, useMemo, useState } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { salesErrorText } from "./sales-error";
import type { ApiResult } from "@/lib/api/server";
import type { StageRegisterEntry, ValuationAssumptions, ValuationBucket, ValuationStage } from "@/lib/api/sales-valuation-server";
import { saveValuationAction, type ValuationActionState } from "./valuation-actions";
import { displayStageLabel, fmtValuationSavedAt, storedStageLabel } from "./valuation-display";
import Alert from "@mui/material/Alert";
import Autocomplete from "@mui/material/Autocomplete";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Chip from "@mui/material/Chip";
import IconButton from "@mui/material/IconButton";
import Stack from "@mui/material/Stack";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";

const INITIAL: ValuationActionState = { status: "idle", code: "", message: "", ticket: 0 };

/** A stage as the screen holds it: the authored row plus the id that keeps its inputs mounted. */
type EditRow = ValuationStage & { rid: string };
const GENDERS = [
  { key: "female", copyKey: "valuation.gender.female" },
  { key: "male", copyKey: "valuation.gender.male" },
] as const;

// The same slug the backend derives a new stage's key from, so the figures typed beside a stage
// being added land on the key it is stored under. The backend keeps a key it is given, and refuses
// one that collides with a stage already there.
function stageKeyFromLabel(label: string): string {
  return label
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
}

function normalizeMatch(s: string): string {
  return s.toUpperCase().replace(/[^A-Z0-9]/g, "");
}

export function ValuationSection({
  pageContract,
  result,
  canEdit,
  disabledReason,
}: {
  pageContract: AdminUiPageContract;
  result: ApiResult<ValuationAssumptions>;
  canEdit: boolean;
  disabledReason: string;
}) {
  const [state, formAction, pending] = useActionState(saveValuationAction, INITIAL);
  // The row on screen is the last one SAVED when there is one, else the one the page loaded with:
  // the hidden row_version below must be the fence the next save is judged against.
  const v = state.saved ?? (result.ok ? result.data : undefined);
  // Each row carries a client-side id so React keeps the same inputs while the farm edits it. The
  // stage KEY cannot serve: it is blank on a row being added and would otherwise be re-derived on
  // every keystroke, remounting the input the name is being typed into.
  const [stages, setStages] = useState<EditRow[] | null>(null);
  const [nextRid, setNextRid] = useState(1);
  // A save replaces the stage list with what was stored -- including the keys the backend assigned
  // to rows that were added -- so edits are never applied on top of a list the server did not take.
  const [appliedTicket, setAppliedTicket] = useState(0);
  if (state.status === "success" && state.ticket !== appliedTicket) {
    setAppliedTicket(state.ticket);
    setStages(null);
  }
  const register: StageRegisterEntry[] = v?.stage_register ?? [];
  // A seeded stage labelled with its raw code (K0..K3) shows the register's name for that code;
  // the stored label goes back on save unless the farm edits the name (valuation-display.ts).
  const storedLabels = useMemo(() => new Map((v?.stages ?? []).map((st) => [st.stage, st.label])), [v]);
  const rows: EditRow[] = stages ?? (v?.stages ?? []).map((st) => ({ ...st, label: displayStageLabel(st.stage, st.label, register), rid: st.stage }));

  const byBucket = useMemo(() => {
    const m = new Map<string, ValuationBucket>();
    for (const b of v?.buckets ?? []) m.set(b.bucket, b);
    return m;
  }, [v]);

  // A register entry belongs to ONE valuation stage, so an entry already placed is not offered
  // again -- the screen cannot compose the write the backend would refuse.
  const placed = useMemo(() => new Set(rows.flatMap((s) => s.matches.map(normalizeMatch))), [rows]);
  const unplaced = register.filter((e) => !placed.has(normalizeMatch(e.code)));
  const unplacedWithAnimals = unplaced.filter((e) => e.live_animals > 0);

  if (!result.ok || !v) {
    return (
      <Card component="section" aria-label={copy(pageContract, "section.valuation.aria")}>
        <CardHeader title={copy(pageContract, "section.valuation.title")} />
        <Box sx={{ p: 3 }}>
          <Alert severity="error">{result.ok ? copy(pageContract, "error.load") : salesErrorText(result.error, copy(pageContract, "error.load"))}</Alert>
        </Box>
      </Card>
    );
  }

  const edit = (i: number, patch: Partial<EditRow>) => setStages(rows.map((s, n) => (n === i ? { ...s, ...patch } : s)));
  const addRow = (row: Omit<EditRow, "rid">) => {
    setStages([...rows, { ...row, rid: `new_${nextRid}` }]);
    setNextRid(nextRid + 1);
  };
  const message = state.status === "success" ? copy(pageContract, "valuation.saved") : state.status === "error" ? state.message || copy(pageContract, "valuation.error") : "";

  // The register's own name for an entry ("Warmup (K4)"), and its live head count as a caption.
  const entryLabel = (code: string): string => {
    const e = register.find((r) => r.code === code);
    if (!e) return code;
    return e.label === e.code ? e.code : `${e.label} (${e.code})`;
  };
  const liveOf = (code: string): number => register.find((r) => r.code === code)?.live_animals ?? 0;
  const numberCellSx = { minWidth: 112 } as const;

  return (
    // Template account-settings card (Card + CardHeader, form body on p: 3, actions row at the foot).
    <Card component="section" aria-label={copy(pageContract, "section.valuation.aria")} data-testid="valuation-section">
      <CardHeader title={copy(pageContract, "section.valuation.title")} subheader={copy(pageContract, "section.valuation.sub")} />
      <Box sx={{ p: 3 }}>
        <form action={formAction} aria-busy={pending}>
          <Stack spacing={3}>
          {!canEdit ? <Alert severity="info">{disabledReason}</Alert> : null}
          <HiddenField name="row_version" value={v.row_version} />
          <HiddenField
            name="stages"
            // `field_key` is what the weight/price inputs on this row are NAMED after, and it stays
            // the row's id so the name does not change under the cursor while a stage is being
            // typed. `stage` is the key it will be STORED under, derived from the label here the
            // same way the backend derives it -- posting the row id would have written `new_1`
            // into the database as a stage key, and the next stage added after a reload would
            // collide with it and be refused.
            value={JSON.stringify(
              rows.map((s, i) => ({
                stage: s.stage || stageKeyFromLabel(s.label),
                field_key: s.stage || s.rid,
                label: storedLabels.has(s.stage) ? storedStageLabel(s.stage, storedLabels.get(s.stage) ?? "", s.label, register) : s.label,
                display_order: i + 1,
                matches: s.matches,
              })),
            )}
          />
          {/* Wide table: scrolls sideways inside the card (template Scrollbar), never clips a cell. */}
          <Box sx={{ mx: -3 }}>
          <Scrollbar>
            <Table size="small" sx={{ minWidth: 960, "& td, & th": { verticalAlign: "top" } }}>
              <TableHead>
                <TableRow>
                  <TableCell>{copy(pageContract, "valuation.stage")}</TableCell>
                  <TableCell>{copy(pageContract, "valuation.covers")}</TableCell>
                  {GENDERS.map((g) => (
                    <TableCell key={g.key} colSpan={2}>
                      {copy(pageContract, g.copyKey)}
                    </TableCell>
                  ))}
                  <TableCell aria-label={copy(pageContract, "valuation.stage.remove")} />
                </TableRow>
                <TableRow>
                  <TableCell />
                  <TableCell />
                  {GENDERS.map((g) => [
                    <TableCell key={`${g.key}-w`} sx={{ typography: "caption", color: "text.secondary" }}>
                      {copy(pageContract, "valuation.fixed_weight")}
                    </TableCell>,
                    <TableCell key={`${g.key}-p`} sx={{ typography: "caption", color: "text.secondary" }}>
                      {copy(pageContract, "valuation.price_per_kg")}
                    </TableCell>,
                  ])}
                  <TableCell />
                </TableRow>
              </TableHead>
              <TableBody>
                {rows.map((s, i) => {
                  // A row being added is named by its id until the farm leaves the name field; the
                  // key settles ONCE, on blur, so the figures typed beside it land on it.
                  const key = s.stage || s.rid;
                  return (
                    <TableRow key={s.rid} data-stage={key}>
                      <TableCell sx={{ minWidth: 180 }}>
                        <TextField
                          size="small"
                          fullWidth
                          value={s.label}
                          onChange={(e) => edit(i, { label: e.target.value })}
                          onBlur={(e) => {
                            if (!s.stage && e.target.value.trim()) edit(i, { stage: stageKeyFromLabel(e.target.value) });
                          }}
                          required
                          disabled={!canEdit}
                          slotProps={{ htmlInput: { maxLength: 60, "aria-label": copy(pageContract, "valuation.stage"), "data-testid": `valuation-stage-label-${key}` } }}
                        />
                      </TableCell>
                      <TableCell sx={{ minWidth: 260 }}>
                        {/* Covers: template Autocomplete multiple (chips). A register entry already
                            placed on another stage is not offered -- the backend would refuse it. */}
                        <Autocomplete
                          multiple
                          size="small"
                          disableClearable
                          disabled={!canEdit}
                          value={s.matches}
                          options={[...s.matches, ...unplaced.map((e) => e.code)]}
                          getOptionLabel={(code) => entryLabel(code)}
                          filterSelectedOptions
                          onChange={(_, next) => edit(i, { matches: next })}
                          renderOption={(props, code) => {
                            const { key: optionKey, ...rest } = props as typeof props & { key: string };
                            return (
                              <li key={optionKey} {...rest}>
                                {entryLabel(code)}
                                {liveOf(code) > 0 ? ` · ${liveOf(code)}` : ""}
                              </li>
                            );
                          }}
                          slotProps={{ chip: { size: "small" } }}
                          renderInput={(params) => (
                            <TextField
                              {...params}
                              placeholder={s.matches.length ? undefined : copy(pageContract, "valuation.covers.add")}
                              slotProps={{ ...params.slotProps, htmlInput: { ...params.slotProps.htmlInput, "aria-label": copy(pageContract, "valuation.covers.add"), "data-testid": `valuation-covers-add-${key}` } }}
                            />
                          )}
                        />
                      </TableCell>
                      {GENDERS.map((g) => {
                        const bucket = `${key}_${g.key}`;
                        const b = byBucket.get(bucket);
                        return [
                          <TableCell key={`${bucket}-w`} sx={numberCellSx}>
                            <TextField
                              size="small"
                              fullWidth
                              name={`weight_${bucket}`}
                              type="number"
                              defaultValue={b?.fixed_weight_kg ?? ""}
                              placeholder={copy(pageContract, "valuation.fixed_weight.measured")}
                              disabled={!canEdit}
                              slotProps={{ htmlInput: { step: "0.1", min: v.limits.fixed_weight_kg_min, max: v.limits.fixed_weight_kg_max, "aria-label": `${copy(pageContract, "valuation.fixed_weight")} · ${s.label} · ${copy(pageContract, g.copyKey)}` } }}
                            />
                          </TableCell>,
                          <TableCell key={`${bucket}-p`} sx={numberCellSx}>
                            <TextField
                              size="small"
                              fullWidth
                              name={`price_${bucket}`}
                              type="number"
                              defaultValue={b?.price_per_kg ?? ""}
                              required
                              disabled={!canEdit}
                              slotProps={{ htmlInput: { step: "1", min: v.limits.price_per_kg_min, max: v.limits.price_per_kg_max, "aria-label": `${copy(pageContract, "valuation.price_per_kg")} · ${s.label} · ${copy(pageContract, g.copyKey)}`, "data-testid": `valuation-price-${bucket}` } }}
                            />
                          </TableCell>,
                        ];
                      })}
                      <TableCell align="right">
                        {canEdit ? (
                          <IconButton
                            aria-label={`${copy(pageContract, "valuation.stage.remove")} ${s.label}`}
                            data-testid={`valuation-stage-remove-${key}`}
                            onClick={() => setStages(rows.filter((_, n) => n !== i))}
                          >
                            <Iconify icon="mingcute:close-line" />
                          </IconButton>
                        ) : null}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </Scrollbar>
          </Box>
          <Typography variant="caption" sx={{ color: "text.secondary" }}>{copy(pageContract, "valuation.fixed_weight.hint")}</Typography>

          {/* The stages nothing values, with the animals standing in them. This is the screen's own
              answer to the question that made the stage list data in the first place. */}
          {canEdit && unplacedWithAnimals.length > 0 ? (
            <Alert severity="warning" data-testid="valuation-unvalued" sx={{ "& .MuiAlert-message": { minWidth: 0 } }}>
              <Box sx={{ mb: 1 }}>{copy(pageContract, "valuation.unvalued")}</Box>
              <Stack direction="row" sx={{ flexWrap: "wrap", gap: 1 }}>
                {unplacedWithAnimals.map((e) => (
                  <Chip
                    key={e.code}
                    variant="outlined"
                    icon={<Iconify icon="mingcute:add-line" />}
                    label={`${e.label === e.code ? e.code : `${e.label} (${e.code})`} · ${e.live_animals}`}
                    data-testid={`valuation-add-register-${e.code}`}
                    onClick={() => addRow({ stage: stageKeyFromLabel(e.label === e.code ? e.code : e.label), label: e.label === e.code ? e.code : e.label, display_order: rows.length + 1, matches: [e.code] })}
                  />
                ))}
              </Stack>
            </Alert>
          ) : null}

          <TextField
            name="unsold_stock_price_rupees"
            type="number"
            label={copy(pageContract, "valuation.unsold_price")}
            helperText={copy(pageContract, "valuation.unsold_price.hint")}
            defaultValue={v.unsold_stock_price_rupees ?? ""}
            disabled={!canEdit}
            sx={{ maxWidth: { sm: 360 } }}
            slotProps={{ htmlInput: { step: "1", min: v.limits.unsold_stock_price_min, max: v.limits.unsold_stock_price_max } }}
          />
          <Stack direction="row" sx={{ flexWrap: "wrap", alignItems: "center", gap: 1.5 }}>
            {canEdit ? (
              <>
                <Button
                  type="button"
                  variant="outlined"
                  color="inherit"
                  startIcon={<Iconify icon="mingcute:add-line" />}
                  data-testid="valuation-stage-add"
                  onClick={() => addRow({ stage: "", label: "", display_order: rows.length + 1, matches: [] })}
                >
                  {copy(pageContract, "valuation.stage.add")}
                </Button>
                <Button type="submit" variant="contained" color="primary" loading={pending} data-testid="valuation-save">
                  {copy(pageContract, "action.valuation.label")}
                </Button>
              </>
            ) : null}
            {v.updated_at ? (
              <Typography variant="caption" sx={{ color: "text.secondary" }}>
                {copy(pageContract, "valuation.updated")} {fmtValuationSavedAt(v.updated_at)}
                {v.updated_by_name ? ` · ${v.updated_by_name}` : ""}
              </Typography>
            ) : null}
            {message ? (
              <Typography role="status" variant="body2" sx={{ color: state.status === "success" ? "success.main" : "error.main" }}>
                {message}
              </Typography>
            ) : null}
          </Stack>
          </Stack>
        </form>
      </Box>
    </Card>
  );
}
