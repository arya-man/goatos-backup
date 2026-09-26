import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { randomUUID } from "node:crypto";
import { ChevronDown, Flag, HeartPulse, PackageCheck, Plus, Truck } from "lucide-react";
import Accordion from "@mui/material/Accordion";
import AccordionSummary from "@mui/material/AccordionSummary";
import AccordionDetails from "@mui/material/AccordionDetails";
import Button from "@mui/material/Button";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import type { ProcurementHFVaccinationEvidence, ProcurementLoadGoat } from "@/lib/api/procurement";
import { ConfirmSubmitButton } from "@/components/confirm-submit-button";
import { fmtDate } from "@/lib/format";
import { copy, optionGroup, optionLabel, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { FormSelect } from "./form-select";
import { contractOptions, listOptions } from "./option-utils";
import { isAcceptedIntake, isProcurementHistoryOnly } from "./work-state";
import {
  acceptIntakeAction,
  addSourceGoatAction,
  arrivalReviewAction,
  createLoadAction,
  dispatchLoadAction,
  preDispatchDecisionAction,
  recordHFVaccinationEvidenceAction,
  recordSourceHealthAction,
  reviewHFVaccinationEvidenceAction,
} from "./actions";
import { OptionalLocationSelect, ParkLocationSelect, ParkShedLocationSelects, type ProcurementLocationOption, type ProcurementLocations } from "./location-selects";
import { DateTimeField } from "@/components/app/date-time-field";
import { ThemedDatePicker } from "@/components/themed-date-picker";
import Alert from "@mui/material/Alert";
import { FormAutocomplete } from "./form-autocomplete";
import { HfRulePicker, type HfRuleOption } from "./hf-rule-picker";
import { NewLoadDrawer, SupplierField, type SupplierOption } from "./new-load-drawer";

// Operator write surface for a load. Every control submits a real server action against a generated
// backend endpoint (idempotency-keyed) — none are display-only. Fields are the template form fields
// (product create/edit: outlined TextField, small, in a 2-column Grid that stacks on phones).

function goatLabel(goat: ProcurementLoadGoat): string {
	return goat.animal_identifier_1 || goat.animal_identifier_2 || (goat.goat_id ? goat.goat_id.slice(0, 8) : "—");
}

// Template Accordion (MUI) per write form, uncontrolled so it renders from this server component.
function Disclosure({
  icon,
  title,
  children,
  id,
  defaultOpen,
}: {
  icon: React.ReactNode;
  title: string;
  children: React.ReactNode;
  id?: string;
  defaultOpen?: boolean;
}) {
  return (
    <Accordion id={id} defaultExpanded={defaultOpen} disableGutters sx={{ scrollMarginTop: 82 }}>
      <AccordionSummary expandIcon={<ChevronDown className="ic" aria-hidden="true" />}>
        <Stack direction="row" spacing={1.5} sx={{ alignItems: "center", minWidth: 0 }}>
          {icon}
          <Typography variant="subtitle1" component="h3">
            {title}
          </Typography>
        </Stack>
      </AccordionSummary>
      <AccordionDetails>{children}</AccordionDetails>
    </Accordion>
  );
}

// One template text field: outlined, small, full width of its grid cell, label always shrunk so a
// placeholder never sits under it.
function Field({
  label,
  name,
  placeholder,
  required,
  type,
  min,
  multiline,
  rows,
}: {
  label: string;
  name: string;
  placeholder?: string;
  required?: boolean;
  type?: "number" | "text";
  min?: number;
  multiline?: boolean;
  rows?: number;
}) {
  return (
    <TextField
      fullWidth
      size="small"
      name={name}
      label={label}
      placeholder={placeholder}
      required={required}
      type={type}
      multiline={multiline}
      rows={rows}
      slotProps={{ inputLabel: { shrink: true }, htmlInput: min === undefined ? undefined : { min } }}
    />
  );
}

function SubmitButton({ children, disabled, title }: { children: React.ReactNode; disabled?: boolean; title?: string }) {
  return (
    <Button type="submit" variant="contained" disabled={disabled} title={title} sx={{ alignSelf: "flex-start" }}>
      {children}
    </Button>
  );
}

// A stable idempotency key, minted once when the form is server-rendered and submitted as a hidden field.
// A double-submit/retry of the same rendered form replays the same key, so the backend returns the original
// result instead of writing twice (e.g. no duplicate source animal); a fresh render = a new key = a new
// logical request. The server action reads this via formIdempotencyKey rather than minting per call.
function IdempotencyKeyField() {
  return <input type="hidden" name="idempotency_key" value={randomUUID()} />;
}


function goatOptions(goats: readonly ProcurementLoadGoat[], pageContract: AdminUiPageContract) {
  return listOptions(
    goats,
    (goat) => goat.goat_id,
    (goat) => `${goatLabel(goat)} · ${optionLabel(pageContract, "proc_purpose", goat.purpose)}`,
  );
}

export function NewLoadForm({
  returnTo,
  pageContract,
  origins,
  suppliers,
}: {
  returnTo: string;
  pageContract: AdminUiPageContract;
  origins: ProcurementLocationOption[];
  suppliers: SupplierOption[];
}) {
  return (
    <NewLoadDrawer
      label={copy(pageContract, "form.new_load.title")}
      closeLabel={copy(pageContract, "action.cancel")}
    >
      <form action={createLoadAction}>
        <IdempotencyKeyField />
        <input type="hidden" name="return_to" value={returnTo} />
        <Stack spacing={3} sx={{ p: 2.5 }}>
          <SupplierField
            label={copy(pageContract, "field.source_party_id")}
            placeholder={copy(pageContract, "placeholder.source_party_id")}
            options={suppliers}
          />
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, sm: 8 }}>
              <OptionalLocationSelect
                label={copy(pageContract, "field.source_location_id")}
                name="source_location_id"
                locations={origins}
                pageContract={pageContract}
              />
            </Grid>
            <Grid size={{ xs: 12, sm: 4 }}>
              <Field
                label={copy(pageContract, "field.expected_count")}
                name="expected_count"
                type="number"
                min={0}
                placeholder={copy(pageContract, "placeholder.zero")}
              />
            </Grid>
            <Grid size={12}>
              <ThemedDatePicker
                name="purchase_date"
                label={copy(pageContract, "field.purchase_date")}
                previousMonthLabel={copy(pageContract, "date.prev_month", "Previous month")}
                nextMonthLabel={copy(pageContract, "date.next_month", "Next month")}
                invalidDateText={copy(pageContract, "date.invalid", "Pick a valid date")}
              />
            </Grid>
            <Grid size={12}>
              <DateTimeField
                name="planned_dispatch_at"
                label={copy(pageContract, "field.planned_dispatch")}
                hourLabel={copy(pageContract, "field.hour", "Hour")}
                minuteLabel={copy(pageContract, "field.minute", "Minute")}
                previousMonthLabel={copy(pageContract, "date.previous_month", "Previous month")}
                nextMonthLabel={copy(pageContract, "date.next_month", "Next month")}
                invalidDateText={copy(pageContract, "date.invalid", "Pick a valid date")}
              />
            </Grid>
          </Grid>
          <Field label={copy(pageContract, "field.notes")} name="notes" placeholder={copy(pageContract, "placeholder.optional")} />
          <SubmitButton>{copy(pageContract, "action.create_load")}</SubmitButton>
        </Stack>
      </form>
    </NewLoadDrawer>
  );
}

export function LoadWriteActions({
  loadId,
  goats,
  hfEvidence = [],
  hfRuleOptions = [],
  returnTo,
  pageContract,
  locations,
  defaultFromLocationId = "",
}: {
  loadId: string;
  goats: ProcurementLoadGoat[];
  hfEvidence?: ProcurementHFVaccinationEvidence[];
  /** Published vaccination schedule doses; the evidence form picks one instead of typing ids. */
  hfRuleOptions?: HfRuleOption[];
  returnTo: string;
  pageContract: AdminUiPageContract;
  locations: ProcurementLocations;
  defaultFromLocationId?: string;
}) {
  // Per-goat source health + pre-dispatch decision belong to goats still inside source entry — not to
  // terminal (rejected/dead/sold/lost) or already-accepted-intake goats.
  const actionableGoats = goats.filter((g) => !isProcurementHistoryOnly(g.current_state) && !isAcceptedIntake(g.current_state));
  const locationBlockReason = !locations.available
    ? copy(pageContract, "location.locations_unavailable")
    : locations.parks.length === 0
      ? copy(pageContract, "location.no_parks")
      : "";
  const parkActionDisabled = locationBlockReason !== "";
  const intakeBlockReason = locationBlockReason || (locations.sheds.length === 0 ? copy(pageContract, "location.no_sheds") : "");
  const intakeDisabled = intakeBlockReason !== "";
  const loadGoatOptions = goatOptions(goats, pageContract);
  const goatById = new Map(goats.map((goat) => [goat.goat_id, goat]));

  return (
    <>
      {locationBlockReason ? (
        <Alert severity="warning" role="alert">
          {locationBlockReason}
        </Alert>
      ) : null}
      <Disclosure icon={<Plus className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />} title={copy(pageContract, "form.add_goat.title")}>
        <form action={addSourceGoatAction}>
          <IdempotencyKeyField />
          <input type="hidden" name="return_to" value={returnTo} />
          <input type="hidden" name="load_id" value={loadId} />
          <Stack spacing={2}>
            <Grid container spacing={2}>
              <Grid size={{ xs: 12, sm: 6 }}>
                <Field label={copy(pageContract, "field.animal_identifier_1")} name="animal_identifier_1" required placeholder={copy(pageContract, "placeholder.animal_identifier_1")} />
              </Grid>
              <Grid size={{ xs: 12, sm: 6 }}>
                <Field label={copy(pageContract, "field.animal_identifier_2")} name="animal_identifier_2" placeholder={copy(pageContract, "placeholder.animal_identifier_2")} />
              </Grid>
              <Grid size={{ xs: 12, sm: 6 }}>
                <FormSelect
                  size="small"
                  fullWidth
                  label={copy(pageContract, "field.species")}
                  name="species"
                  defaultValue=""
                  required
                  options={contractOptions(pageContract, "proc_species", copy(pageContract, "placeholder.species"))}
                />
              </Grid>
              <Grid size={{ xs: 12, sm: 6 }}>
                <FormSelect
                  size="small"
                  fullWidth
                  label={copy(pageContract, "field.sex")}
                  name="sex"
                  defaultValue=""
                  required
                  options={contractOptions(pageContract, "proc_sex", copy(pageContract, "placeholder.sex"))}
                />
              </Grid>
              <Grid size={{ xs: 12, sm: 6 }}>
                <FormSelect size="small" fullWidth label={copy(pageContract, "field.selection_state")} name="selection_state" defaultValue="source_only" options={contractOptions(pageContract, "proc_selection_state")} />
              </Grid>
              <Grid size={{ xs: 12, sm: 6 }}>
                <FormSelect size="small" fullWidth label={copy(pageContract, "field.health_state")} name="health_state" defaultValue="pending" options={contractOptions(pageContract, "proc_health_state")} />
              </Grid>
              <Grid size={{ xs: 12, sm: 6 }}>
                <FormSelect size="small" fullWidth label={copy(pageContract, "field.ownership")} name="ownership_state" defaultValue="pending" options={contractOptions(pageContract, "proc_ownership_state")} />
              </Grid>
              <Grid size={{ xs: 12, sm: 6 }}>
                <FormSelect size="small" fullWidth label={copy(pageContract, "field.purpose")} name="purpose" defaultValue="unspecified" options={contractOptions(pageContract, "proc_purpose")} />
              </Grid>
              <Grid size={{ xs: 12, sm: 4 }}>
                <Field label={copy(pageContract, "field.warmup_days")} name="warmup_days" type="number" min={0} placeholder={copy(pageContract, "placeholder.warmup_days")} />
              </Grid>
              <Grid size={{ xs: 12, sm: 8 }}>
                <OptionalLocationSelect
                  label={copy(pageContract, "field.holding_location_id")}
                  name="holding_location_id"
                  locations={locations.origins}
                  pageContract={pageContract}
                />
              </Grid>
            </Grid>
            <Typography variant="caption" sx={{ color: "text.secondary" }}>
              {copy(pageContract, "note.source_goat_identity")}
            </Typography>
            <SubmitButton>{copy(pageContract, "action.add_source_goat")}</SubmitButton>
          </Stack>
        </form>
      </Disclosure>

      {/* Holding-farm vaccination evidence — this is the mock's supplier-warmup vaccination action surface.
          It stays in Procurement Source Entry, not Preventive Care (PC) / Vaccination. Imported+trusted evidence is later
          consumed by the accepted-intake handoff/no-double-dose path. */}
      <Disclosure
        id="hf-evidence"
        defaultOpen
        icon={<HeartPulse className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />}
        title={copy(pageContract, "form.hf_evidence.title")}
      >
        {goats.length === 0 ? (
          <Typography variant="body2" sx={{ color: "text.secondary" }}>
            {copy(pageContract, "empty.add_goats_first")}
          </Typography>
        ) : (
          <Stack spacing={3}>
            <form action={recordHFVaccinationEvidenceAction}>
              <IdempotencyKeyField />
              <input type="hidden" name="return_to" value={returnTo} />
              <input type="hidden" name="load_id" value={loadId} />
              <Stack spacing={2}>
                <Grid container spacing={2}>
                  <Grid size={12}>
                    <FormSelect
                      size="small"
                      fullWidth
                      label={copy(pageContract, "field.goat_in_load")}
                      name="goat_id"
                      required
                      defaultValue={goats[0]?.goat_id ?? ""}
                      options={loadGoatOptions}
                    />
                  </Grid>
                  <HfRulePicker
                    label={copy(pageContract, "field.protocol_version_id")}
                    emptyLabel={copy(pageContract, "placeholder.protocol_version_id")}
                    doseLabel={copy(pageContract, "field.dose_code")}
                    dosePlaceholder={copy(pageContract, "placeholder.dose_code")}
                    options={hfRuleOptions}
                  />
                  <Grid size={12}>
                    <DateTimeField
                      name="administered_at"
                      required
                      label={copy(pageContract, "field.administered_at_hf")}
                      hourLabel={copy(pageContract, "field.hour", "Hour")}
                      minuteLabel={copy(pageContract, "field.minute", "Minute")}
                      previousMonthLabel={copy(pageContract, "date.previous_month", "Previous month")}
                      nextMonthLabel={copy(pageContract, "date.next_month", "Next month")}
                      invalidDateText={copy(pageContract, "date.invalid", "Pick a valid date")}
                    />
                  </Grid>
                  <Grid size={{ xs: 12, sm: 6 }}>
                    <Field label={copy(pageContract, "field.vaccine_name")} name="vaccine_name" placeholder={copy(pageContract, "placeholder.optional")} />
                  </Grid>
                  <Grid size={{ xs: 12, sm: 6 }}>
                    <Field label={copy(pageContract, "field.lot_number")} name="lot_number" placeholder={copy(pageContract, "placeholder.optional")} />
                  </Grid>
                  <Grid size={12}>
                    <Field label={copy(pageContract, "field.source_ref")} name="source_ref" placeholder={copy(pageContract, "placeholder.source_ref")} />
                  </Grid>
                </Grid>
                <SubmitButton>{copy(pageContract, "action.import_hf_evidence")}</SubmitButton>
              </Stack>
            </form>

            {hfEvidence.length === 0 ? (
              <Alert severity="info">{copy(pageContract, "empty.hf_evidence")}</Alert>
            ) : (
              <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "table.hf_evidence.aria")}>
                <Table>
                  <TableHead>
                    <TableRow>
                      <TableCell component="th">{copy(pageContract, "table.hf_evidence.goat")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "table.hf_evidence.dose")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "table.hf_evidence.administered")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "table.hf_evidence.evidence")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "table.hf_evidence.review")}</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {hfEvidence.map((evidence) => {
                      const goat = goatById.get(evidence.goat_id);
                      return (
                        <TableRow key={evidence.evidence_id}>
                          <TableCell>
                            <span className="gid">{goat ? goatLabel(goat) : evidence.goat_id.slice(0, 8)}</span>
                          </TableCell>
                          <TableCell>
                            <b>{evidence.dose_code}</b>
                            <div className="muted small">{evidence.vaccine_name || copy(pageContract, "label.vaccine_name_not_set")}</div>
                          </TableCell>
                          <TableCell className="muted">{fmtDate(evidence.administered_at)}</TableCell>
                          <TableCell className="muted small">
                            {evidence.proof_ref_id ? copy(pageContract, "label.proof") : copy(pageContract, "label.proof_ref_not_set")}
                          </TableCell>
                          <TableCell>
                            {evidence.review_status === "trusted" ? (
                              <span className="muted small">{copy(pageContract, "label.trusted_locked")}</span>
                            ) : (
                              <form action={reviewHFVaccinationEvidenceAction}>
                                <IdempotencyKeyField />
                                <input type="hidden" name="return_to" value={returnTo} />
                                <input type="hidden" name="load_id" value={loadId} />
                                <input type="hidden" name="evidence_id" value={evidence.evidence_id} />
                                <input type="hidden" name="expected_row_version" value={evidence.row_version} />
                                <Stack direction="row" spacing={1} sx={{ alignItems: "center", flexWrap: "wrap", rowGap: 1 }}>
                                  <FormSelect
                                    size="small"
                                    label={copy(pageContract, "field.review_status")}
                                    name="review_status"
                                    defaultValue="trusted"
                                    minWidth={140}
                                    options={contractOptions(pageContract, "proc_hf_review_status")}
                                  />
                                  <TextField size="small" name="review_reason" label={copy(pageContract, "field.reason")} placeholder={copy(pageContract, "placeholder.reason")} slotProps={{ inputLabel: { shrink: true } }} sx={{ width: 170 }} />
                                  <Button type="submit" variant="outlined">{copy(pageContract, "action.review")}</Button>
                                </Stack>
                              </form>
                            )}
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </div>
            )}
          </Stack>
        )}
      </Disclosure>

      {/* Pre-dispatch section — per-goat source health + accept/reject-before-truck/defer/block. */}
      <Disclosure icon={<Flag className="ic" style={{ color: "var(--amber)" }} aria-hidden="true" />} title={copy(pageContract, "form.pre_dispatch.title")}>
        {actionableGoats.length === 0 ? (
          <Typography variant="body2" sx={{ color: "text.secondary" }}>
            {copy(pageContract, "empty.pre_dispatch")}
          </Typography>
        ) : (
          <Stack spacing={2}>
            {actionableGoats.map((goat) => (
              <Stack key={goat.load_goat_id} spacing={2} sx={{ border: 1, borderColor: "divider", borderRadius: 1.5, p: 2 }}>
                <Stack direction="row" spacing={1} sx={{ alignItems: "center" }}>
                  <HeartPulse className="ic" style={{ color: "var(--brand-d)" }} aria-hidden="true" />
                  <Typography variant="subtitle2">{goatLabel(goat)}</Typography>
                </Stack>
                {/* Source health */}
                <form action={recordSourceHealthAction}>
                  <IdempotencyKeyField />
                  <input type="hidden" name="return_to" value={returnTo} />
                  <input type="hidden" name="load_id" value={loadId} />
                  <input type="hidden" name="goat_id" value={goat.goat_id} />
                  <Grid container spacing={2} sx={{ alignItems: "center" }}>
                    <Grid size={{ xs: 12, sm: 4 }}>
                      <FormSelect size="small" fullWidth label={copy(pageContract, "field.source_health")} name="health_state" defaultValue="passed" options={contractOptions(pageContract, "proc_health_state")} />
                    </Grid>
                    <Grid size={{ xs: 12, sm: 5 }}>
                      <Field label={copy(pageContract, "field.reason")} name="reason" placeholder={copy(pageContract, "placeholder.optional")} />
                    </Grid>
                    <Grid size={{ xs: 12, sm: 3 }}>
                      <Button type="submit" variant="outlined" fullWidth>{copy(pageContract, "action.record_health")}</Button>
                    </Grid>
                  </Grid>
                </form>
                {/* Pre-dispatch decision */}
                <form action={preDispatchDecisionAction}>
                  <IdempotencyKeyField />
                  <input type="hidden" name="return_to" value={returnTo} />
                  <input type="hidden" name="load_id" value={loadId} />
                  <input type="hidden" name="goat_id" value={goat.goat_id} />
                  <Grid container spacing={2} sx={{ alignItems: "center" }}>
                    <Grid size={{ xs: 12, sm: 4 }}>
                      <FormSelect size="small" fullWidth label={copy(pageContract, "field.pre_dispatch")} name="decision_type" defaultValue="accepted" options={contractOptions(pageContract, "proc_decision_type")} />
                    </Grid>
                    <Grid size={{ xs: 12, sm: 5 }}>
                      <Field label={copy(pageContract, "field.reason")} name="reason" placeholder={copy(pageContract, "placeholder.optional")} />
                    </Grid>
                    <Grid size={{ xs: 12, sm: 3 }}>
                      <ConfirmSubmitButton confirmLabel={copy(pageContract, "confirm.ok", "Confirm")} cancelLabel={copy(pageContract, "action.cancel")} dialogTitle={copy(pageContract, "confirm.title", "Please confirm")} className="btn sm" message={`${copy(pageContract, "confirm.pre_dispatch.prefix")} ${goatLabel(goat)}? ${copy(pageContract, "confirm.pre_dispatch.suffix")}`}>
                        {copy(pageContract, "action.record_decision")}
                      </ConfirmSubmitButton>
                    </Grid>
                  </Grid>
                </form>
              </Stack>
            ))}
          </Stack>
        )}
      </Disclosure>

      {/* Dispatch / transit */}
      <Disclosure icon={<Truck className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />} title={copy(pageContract, "form.dispatch.title")}>
        <form action={dispatchLoadAction}>
          <IdempotencyKeyField />
          <input type="hidden" name="return_to" value={returnTo} />
          <input type="hidden" name="load_id" value={loadId} />
          <Stack spacing={2}>
            <Grid container spacing={2}>
              <Grid size={{ xs: 12, sm: 6 }}>
                <ParkLocationSelect label={copy(pageContract, "field.to_location_id")} name="to_location_id" parks={locations.parks} pageContract={pageContract} />
              </Grid>
              <Grid size={{ xs: 12, sm: 6 }}>
                <OptionalLocationSelect label={copy(pageContract, "field.from_location_id")} name="from_location_id" locations={locations.origins} pageContract={pageContract} defaultValue={defaultFromLocationId} />
              </Grid>
              <Grid size={12}>
                <FormAutocomplete label={copy(pageContract, "field.goat_ids")} name="goat_ids" placeholder={copy(pageContract, "placeholder.goat_ids_dispatch")} options={loadGoatOptions} />
              </Grid>
              <Grid size={12}>
                <DateTimeField
                  name="dispatched_at"
                  label={copy(pageContract, "field.dispatched_at")}
                  hourLabel={copy(pageContract, "field.hour", "Hour")}
                  minuteLabel={copy(pageContract, "field.minute", "Minute")}
                  previousMonthLabel={copy(pageContract, "date.previous_month", "Previous month")}
                  nextMonthLabel={copy(pageContract, "date.next_month", "Next month")}
                  invalidDateText={copy(pageContract, "date.invalid", "Pick a valid date")}
                />
              </Grid>
              <Grid size={12}>
                <Field label={copy(pageContract, "field.dispatch_proof_ref_id")} name="proof_ref_id" placeholder={copy(pageContract, "placeholder.dispatch_proof_ref_id")} />
              </Grid>
            </Grid>
            <SubmitButton disabled={parkActionDisabled} title={locationBlockReason || undefined}>{copy(pageContract, "action.record_dispatch")}</SubmitButton>
          </Stack>
        </form>
      </Disclosure>

      {/* Arrival gate review */}
      <Disclosure icon={<Flag className="ic" style={{ color: "var(--purple)" }} aria-hidden="true" />} title={copy(pageContract, "form.arrival_review.title")}>
        <form action={arrivalReviewAction}>
          <IdempotencyKeyField />
          <input type="hidden" name="return_to" value={returnTo} />
          <input type="hidden" name="load_id" value={loadId} />
          <Stack spacing={2}>
            <Grid container spacing={2}>
              <Grid size={{ xs: 12, sm: 6 }}>
                <ParkLocationSelect label={copy(pageContract, "field.park_location_id")} name="park_location_id" parks={locations.parks} pageContract={pageContract} />
              </Grid>
              <Grid size={{ xs: 12, sm: 6 }}>
                <FormSelect size="small" fullWidth label={copy(pageContract, "field.review_status")} name="status" defaultValue="pending" options={contractOptions(pageContract, "proc_arrival_status")} />
              </Grid>
              {optionGroup(pageContract, "proc_arrival_counts").map((count) => (
                <Grid key={count.key} size={{ xs: 6, sm: 3 }}>
                  <Field label={count.label} name={count.key} type="number" min={0} placeholder={copy(pageContract, "placeholder.zero")} />
                </Grid>
              ))}
              <Grid size={12}>
                <Field label={copy(pageContract, "field.arrival_rows")} name="goats" multiline rows={4} placeholder={copy(pageContract, "placeholder.arrival_rows")} />
                <Typography variant="caption" component="p" sx={{ color: "text.secondary", mt: 0.75 }}>
                  {copy(pageContract, "note.arrival_rows")}
                </Typography>
              </Grid>
            </Grid>
            <SubmitButton disabled={parkActionDisabled} title={locationBlockReason || undefined}>{copy(pageContract, "action.record_arrival_review")}</SubmitButton>
          </Stack>
        </form>
      </Disclosure>

      {/* Accept intake (load-level) */}
      <Disclosure icon={<PackageCheck className="ic" style={{ color: "var(--brand-d)" }} aria-hidden="true" />} title={copy(pageContract, "form.accept_intake.title")}>
        <form action={acceptIntakeAction}>
          <IdempotencyKeyField />
          <input type="hidden" name="return_to" value={returnTo} />
          <input type="hidden" name="load_id" value={loadId} />
          <Stack spacing={2}>
            <Grid container spacing={2}>
              <Grid size={12}>
                <FormAutocomplete label={copy(pageContract, "field.goat_ids")} name="goat_ids" placeholder={copy(pageContract, "placeholder.goat_ids_intake")} options={loadGoatOptions} />
              </Grid>
              <ParkShedLocationSelects parks={locations.parks} sheds={locations.sheds} pageContract={pageContract} />
              <Grid size={{ xs: 12, sm: 6 }}>
                <ThemedDatePicker
                  name="entry_date"
                  label={copy(pageContract, "field.entry_date")}
                  previousMonthLabel={copy(pageContract, "date.prev_month", "Previous month")}
                  nextMonthLabel={copy(pageContract, "date.next_month", "Next month")}
                  invalidDateText={copy(pageContract, "date.invalid", "Pick a valid date")}
                />
              </Grid>
              <Grid size={{ xs: 12, sm: 6 }}>
                <FormSelect size="small" fullWidth label={copy(pageContract, "field.intake_health_signal")} name="intake_health_signal" defaultValue="clear" options={contractOptions(pageContract, "proc_intake_signal")} />
              </Grid>
            </Grid>
            <Typography variant="caption" sx={{ color: "text.secondary" }}>
              {copy(pageContract, "label.pc_handoff_note")}
            </Typography>
            <ConfirmSubmitButton confirmLabel={copy(pageContract, "confirm.ok", "Confirm")} cancelLabel={copy(pageContract, "action.cancel")} dialogTitle={copy(pageContract, "confirm.title", "Please confirm")} className="btn p" message={copy(pageContract, "confirm.accept_intake")} disabled={intakeDisabled} title={intakeBlockReason || undefined}>
              {copy(pageContract, "action.accept_intake")}
            </ConfirmSubmitButton>
          </Stack>
        </form>
      </Disclosure>
    </>
  );
}
