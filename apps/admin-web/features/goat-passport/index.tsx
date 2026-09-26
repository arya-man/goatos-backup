import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { randomUUID } from "node:crypto";
import { AlertTriangle, BadgeCheck, FileText, Fingerprint, History, Plus, ShieldCheck } from "lucide-react";
import { redirect } from "next/navigation";
import MuiCard from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import CardContent from "@mui/material/CardContent";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { dateTime, dash, humanizeEnum, joinParts, shortId } from "@/lib/format";
import { PageHeader } from "@/components/app/page-header";
import { Tag, type Tone } from "@/components/ui-primitives";
import "./goat-passport.css";
import { PassportCover } from "./passport-cover";
import { firstAuthRequiredError, getGoatPassport, getGoatTimeline } from "@/lib/api/server";
import { hrefWithoutAction, one, type RouteSearchParams } from "@/lib/search-params";
import { actionFeedbackCopy, copy, optionGroup, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { addIdentifierAction, retireIdentifierAction } from "./actions";
import { PassportConfirmSubmitButton, PassportFormSelect } from "./passport-form-controls";
import TextField from "@mui/material/TextField";
import Alert from "@mui/material/Alert";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";

function identifierTypeLabel(type: string, pageContract: AdminUiPageContract): string {
  if (type === "animal_identifier_1") return copy(pageContract, "label.tag_1");
  if (type === "animal_identifier_2") return copy(pageContract, "label.tag_2");
  return type;
}

function MiniMetric({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="gp-def">
      <dt className="gp-def-k">{label}</dt>
      <dd className="gp-def-v">{value}</dd>
    </div>
  );
}

function lifecycleTone(value: string | null | undefined): Tone {
  const v = String(value ?? "").toLowerCase();
  if (!v) return "mut";
  if (["alive", "active"].includes(v)) return "ok";
  if (["dead", "died", "deceased", "culled"].includes(v)) return "dng";
  if (["sold", "transferred", "merged"].includes(v)) return "info";
  return "mut";
}
function healthTone(value: string | null | undefined): Tone {
  const v = String(value ?? "").toLowerCase();
  if (!v) return "mut";
  if (["healthy", "normal", "ok"].includes(v)) return "ok";
  if (["sick", "critical", "dead"].includes(v)) return "dng";
  if (v.includes("treatment") || v.includes("watch") || v.includes("quarantine")) return "warn";
  return "info";
}

function Notice({ status, actionKey, pageContract }: { status?: string; actionKey?: string; pageContract: AdminUiPageContract }) {
  if (!status) return null;
  if (status === "success") {
    return (
      <div className="note" style={{ marginBottom: 14 }}>
        <Tag tone="ok">{copy(pageContract, "action.done")}</Tag> {actionFeedbackCopy(pageContract, status, actionKey)}
      </div>
    );
  }
  return (
    <Alert severity="error" style={{ marginBottom: 14 }}><div>{actionFeedbackCopy(pageContract, status, actionKey)}</div>
    </Alert>
  );
}

function FormSelect({
  name,
  label,
  options,
  required,
  defaultValue,
  emptyLabel,
}: {
  name: string;
  label: string;
  options: AdminUiOption[];
  required?: boolean;
  defaultValue?: string;
  emptyLabel?: string;
}) {
  return (
    <PassportFormSelect
      name={name}
      label={label}
      required={required}
      defaultValue={defaultValue}
      emptyLabel={emptyLabel}
      options={options.map((option) => ({ value: option.key, label: option.label }))}
    />
  );
}

function FormField({
  name,
  label,
  required,
  defaultValue,
  placeholder,
}: {
  name: string;
  label: string;
  required?: boolean;
  defaultValue?: string;
  placeholder?: string;
}) {
  return (
    <TextField
      fullWidth
      size="small"
      name={name}
      label={label}
      defaultValue={defaultValue}
      placeholder={placeholder}
      slotProps={{ inputLabel: { shrink: true }, htmlInput: { required } }}
    />
  );
}

// Template UserProfileView tabs (sections/user/view/user-profile-view.tsx): tab param drives
// which body renders below the profile-cover Card. Same shape, different sections.
const TAB_PARAM = "tab";
type PassportTab = "" | "identifiers" | "evidence" | "history";
const ALL_TABS: PassportTab[] = ["", "identifiers", "evidence", "history"];
function normalizeTab(value: string | undefined): PassportTab {
  const v = (value ?? "") as PassportTab;
  return ALL_TABS.includes(v) ? v : "";
}
function tabHref(pathname: string, sp: RouteSearchParams, tab: PassportTab): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(sp)) {
    if (key === TAB_PARAM) continue;
    if (Array.isArray(value)) {
      for (const item of value) if (item) next.append(key, item);
    } else if (value) next.set(key, value);
  }
  if (tab) next.set(TAB_PARAM, tab);
  const qs = next.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}

export async function GoatPassportPage({
  goatId,
  searchParams = {},
  pageContract,
}: {
  goatId: string;
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const [result, timeline] = await Promise.all([getGoatPassport(goatId), getGoatTimeline({ goatId, limit: 20 })]);
  const returnTo = hrefWithoutAction(`/goats/${encodeURIComponent(goatId)}`, searchParams);
  const actionStatus = one(searchParams, "action_status");
  const actionKey = one(searchParams, "action_key");
  const authError = firstAuthRequiredError(result, timeline);
  if (authError) {
    redirect(INTERNAL_LOGIN_PATH);
  }

  if (!result.ok) {
    return (
      <div className="screen on">
        <PageHeader title={pageContract.title || copy(pageContract, "fallback.title")} crumbs={[{ label: copy(pageContract, "fallback.title") }]} />
        <Alert severity="error"><div>
            <b>{result.error.code}</b>
            <div className="muted small" style={{ marginTop: 4 }}>
              {result.error.message}
            </div>
          </div>
        </Alert>
      </div>
    );
  }

  const { goat, warnings } = result.data;
  const allWarnings = [...warnings, ...goat.summary.warnings];

  const lifecycle = goat.summary.lifecycle_status;
  const health = goat.summary.health_status;
  const selectedTab = normalizeTab(one(searchParams, TAB_PARAM));
  const passportPath = `/goats/${encodeURIComponent(goatId)}`;
  const secondaryLine = joinParts([goat.summary.breed, humanizeEnum(goat.summary.sex)]);

  const coverCard = (
    <PassportCover
      displayId={goat.display_id}
      secondaryLine={secondaryLine}
      selectedTab={selectedTab}
      tabs={[
        { value: "", href: tabHref(passportPath, searchParams, ""), label: copy(pageContract, "section.summary.title") },
        { value: "identifiers", href: tabHref(passportPath, searchParams, "identifiers"), label: copy(pageContract, "section.identifiers.title") },
        { value: "evidence", href: tabHref(passportPath, searchParams, "evidence"), label: copy(pageContract, "section.evidence.title") },
        { value: "history", href: tabHref(passportPath, searchParams, "history"), label: copy(pageContract, "section.timeline.title") },
      ]}
    />
  );

  const summaryBody = (
    <>
      <MuiCard className="gp-summary" aria-label={copy(pageContract, "section.summary.title")}>
        <CardHeader
          title={
            <span className="gp-card-title">
              <BadgeCheck className="ic" aria-hidden="true" />
              {copy(pageContract, "section.summary.title")}
            </span>
          }
        />
        <CardContent>
        <div className="gp-def-cols">
          <dl className="gp-def-block">
            <MiniMetric label={copy(pageContract, "label.display_id")} value={<span className="gid">{goat.display_id}</span>} />
            <MiniMetric label={copy(pageContract, "label.tag_1")} value={<span className="mono">{dash(goat.summary.animal_identifier_1)}</span>} />
            <MiniMetric label={copy(pageContract, "label.tag_2")} value={<span className="mono">{dash(goat.summary.animal_identifier_2)}</span>} />
            <MiniMetric label={copy(pageContract, "label.breed_sex")} value={joinParts([goat.summary.breed, humanizeEnum(goat.summary.sex)])} />
            <MiniMetric
              label={copy(pageContract, "label.location")}
              value={
                <>
                  <b>{dash(goat.summary.location_path.operational_location_display)}</b>
                  {goat.merged_into_goat_id ? (
                    <>
                      {" "}
                      · {copy(pageContract, "label.merged_into")} <span className="gid">{shortId(goat.merged_into_goat_id)}</span>
                    </>
                  ) : null}
                </>
              }
            />
          </dl>
          <dl className="gp-def-block">
            <MiniMetric label={copy(pageContract, "label.lifecycle")} value={<Tag tone={lifecycleTone(lifecycle)}>{humanizeEnum(lifecycle)}</Tag>} />
            <MiniMetric label={copy(pageContract, "label.health")} value={health ? <Tag tone={healthTone(health)}>{humanizeEnum(health)}</Tag> : dash(null)} />
            <MiniMetric label={copy(pageContract, "label.reproductive")} value={goat.summary.reproductive_status ? humanizeEnum(goat.summary.reproductive_status) : dash(null)} />
            <MiniMetric label={copy(pageContract, "label.growth_cohort")} value={dash(goat.summary.growth_cohort_tag)} />
            <MiniMetric label={copy(pageContract, "label.management")} value={dash(goat.summary.management_stage)} />
          </dl>
        </div>
        </CardContent>
      </MuiCard>

      {allWarnings.length > 0 ? (
        <MuiCard className="gp-warnings">
          <CardHeader
            title={
              <span className="gp-card-title">
                <ShieldCheck className="ic" aria-hidden="true" />
                {copy(pageContract, "section.warnings.title")}
              </span>
            }
            action={<Tag tone="warn">{allWarnings.length}</Tag>}
          />
          <CardContent>
          <div className="feed">
            {allWarnings.map((warning, index) => (
              <div key={`${warning.code}-${index}`} className="fitem">
                <span className="fic" style={{ background: "var(--warnx)", color: "var(--amber)" }}>
                  <AlertTriangle className="ic" />
                </span>
                <div className="tx">
                  <b>{warning.code}</b>
                  <div className="mt">{warning.message}</div>
                  {warning.original_goat_id || warning.redirect_goat_id ? (
                    <div className="mono" style={{ marginTop: 5 }}>
                      {shortId(warning.original_goat_id)} -&gt; {shortId(warning.redirect_goat_id)}
                    </div>
                  ) : null}
                </div>
              </div>
            ))}
          </div>
          </CardContent>
        </MuiCard>
      ) : null}
    </>
  );

  const identifiersBody = (
    <MuiCard className="kit-tablecard gp-identifiers" aria-label={copy(pageContract, "section.identifiers.title")}>
      <CardHeader
        className="gp-tablecard-head"
        title={
          <span className="gp-card-title">
            <Fingerprint className="ic" aria-hidden="true" />
            {copy(pageContract, "section.identifiers.title")}
          </span>
        }
        action={<Tag tone="mut">{goat.identifiers.length}</Tag>}
      />
      {goat.identifiers.length === 0 ? (
        <div className="gp-empty muted small">{copy(pageContract, "empty.identifiers")}</div>
      ) : (
        <div className="tablewrap gp-tablewrap" tabIndex={0} role="group" aria-label={copy(pageContract, "table.identifiers.aria")}>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell component="th">{copy(pageContract, "label.type")}</TableCell>
                <TableCell component="th">{copy(pageContract, "label.value")}</TableCell>
                <TableCell component="th">{copy(pageContract, "label.scope")}</TableCell>
                <TableCell component="th">{copy(pageContract, "label.status")}</TableCell>
                <TableCell component="th">{copy(pageContract, "label.primary")}</TableCell>
                <TableCell component="th">{copy(pageContract, "label.valid_from")}</TableCell>
                <TableCell component="th">{copy(pageContract, "label.action")}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {goat.identifiers.map((identifier) => (
                <TableRow key={identifier.identifier_id}>
                  <TableCell>{identifierTypeLabel(identifier.identifier_type, pageContract)}</TableCell>
                  <TableCell className="mono">{identifier.identifier_value}</TableCell>
                  <TableCell>{humanizeEnum(identifier.scope_key)}</TableCell>
                  <TableCell>
                    <Tag tone={identifier.status === "active" ? "ok" : "mut"}>{humanizeEnum(identifier.status)}</Tag>
                  </TableCell>
                  <TableCell>{humanizeEnum(identifier.is_primary_for_goat ? copy(pageContract, "label.yes") : copy(pageContract, "label.no"))}</TableCell>
                  <TableCell>{dateTime(identifier.valid_from)}</TableCell>
                  <TableCell>
                    {identifier.status === "active" ? (
                      <form action={retireIdentifierAction} style={{ display: "inline" }}>
                        <input type="hidden" name="goat_id" value={goat.goat_id} />
                        <input type="hidden" name="identifier_id" value={identifier.identifier_id} />
                        <input type="hidden" name="row_version" value={goat.row_version} />
                        <input type="hidden" name="idempotency_key" value={randomUUID()} />
                        <input type="hidden" name="return_to" value={returnTo} />
                        <input type="hidden" name="evidence_type" value="identifier" />
                        <input type="hidden" name="evidence_id" value={identifier.identifier_id} />
                        <input type="hidden" name="reason" value={copy(pageContract, "reason.retire_identifier")} />
                        <PassportConfirmSubmitButton
                          message={`${copy(pageContract, "confirm.retire_identifier")} ${identifier.identifier_type} ${identifier.identifier_value}`}
                          confirmLabel={copy(pageContract, "action.retire_identifier")}
                          cancelLabel={copy(pageContract, "action.cancel", "Cancel")}
                          className="btn sm"
                        >
                          {copy(pageContract, "action.retire_identifier")}
                        </PassportConfirmSubmitButton>
                      </form>
                    ) : (
                      <span className="muted small">-</span>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <div className="bd gp-addform">
        <form action={addIdentifierAction} className="cfgform">
          <input type="hidden" name="goat_id" value={goat.goat_id} />
          <input type="hidden" name="row_version" value={goat.row_version} />
          <input type="hidden" name="idempotency_key" value={randomUUID()} />
          <input type="hidden" name="return_to" value={returnTo} />
          <div className="hd" style={{ padding: 0, borderBottom: 0, marginBottom: 2 }}>
            <Plus className="ic" />
            <h3>{copy(pageContract, "section.add_identifier.title")}</h3>
          </div>
          <div
            className="grid goat-identifier-grid"
            style={{ gridTemplateColumns: "repeat(auto-fit, minmax(190px, 1fr))" }}
          >
            <FormSelect name="identifier_type" label={copy(pageContract, "field.identifier_type")} options={optionGroup(pageContract, "identifier_types")} required emptyLabel={copy(pageContract, "label.select")} />
            <FormField name="identifier_value" label={copy(pageContract, "field.identifier_value")} required />
            <FormField name="scope_key" label={copy(pageContract, "field.scope_key")} required placeholder={copy(pageContract, "placeholder.scope_key")} />
            <FormControlLabel className="kit-check gp-check" control={<Checkbox name="is_primary_for_goat" sx={{ p: { xs: 1.5, sm: 1 } }} />} label={<><span>{copy(pageContract, "label.primary")}</span></>} />
          </div>
          <div className="grid g3" style={{ marginTop: 8 }}>
            <EvidenceFields defaultType="goat" defaultID={goat.goat_id} pageContract={pageContract} />
          </div>
          <div style={{ display: "flex", justifyContent: "flex-end", marginTop: 12 }}>
            <button className="btn p">{copy(pageContract, "action.add_identifier")}</button>
          </div>
        </form>
      </div>
    </MuiCard>
  );

  const evidenceBody = (
    <MuiCard className="gp-evidence" aria-label={copy(pageContract, "section.evidence.title")}>
      <CardHeader
        title={
          <span className="gp-card-title">
            <FileText className="ic" aria-hidden="true" />
            {copy(pageContract, "section.evidence.title")}
          </span>
        }
        action={<Tag tone="mut">{goat.evidence_refs.length}</Tag>}
      />
      <CardContent>
        {goat.evidence_refs.length === 0 ? (
          <p className="muted small" style={{ margin: 0 }}>
            {copy(pageContract, "empty.evidence")}
          </p>
        ) : (
          <div className="feed">
            {goat.evidence_refs.map((evidence) => (
              <div key={`${evidence.evidence_type}-${evidence.evidence_id}`} className="fitem">
                <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
                  <FileText className="ic" />
                </span>
                <div className="tx">
                  <b>{evidence.evidence_type}</b>
                  <div className="mono">{evidence.evidence_id}</div>
                  <div className="mt">{dash(evidence.description ?? evidence.source_system)}</div>
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </MuiCard>
  );

  const historyBody = (
    <MuiCard className="gp-timeline" aria-label={copy(pageContract, "section.timeline.title")}>
      <CardHeader
        title={
          <span className="gp-card-title">
            <History className="ic" aria-hidden="true" />
            {copy(pageContract, "section.timeline.title")}
          </span>
        }
        action={<Tag tone="mut">{copy(pageContract, "label.identity_events_table")}</Tag>}
      />
      <CardContent>
        {!timeline.ok ? (
          <Alert severity="error"><div>
              <b>{timeline.error.code}</b>
              <div className="muted small" style={{ marginTop: 4 }}>
                {timeline.error.message}
              </div>
            </div>
          </Alert>
        ) : listOrEmpty(timeline.data.items).length === 0 ? (
          <p className="muted small" style={{ margin: 0 }}>
            {copy(pageContract, "empty.timeline")}
          </p>
        ) : (
          <div className="htl">
            {listOrEmpty(timeline.data.items).map((event) => (
              <div key={event.event_id} className="hrow">
                <span className="hdot t-info" />
                <div className="htx">
                  <b>{humanizeEnum(event.event_type)}</b>
                  <div className="hmeta">
                    {copy(pageContract, "label.occurred")} {dateTime(event.occurred_at)} · {copy(pageContract, "label.recorded")} {dateTime(event.recorded_at)} · {copy(pageContract, "label.evidence")}{" "}
                    {event.evidence_refs.length}
                  </div>
                  <div className="hmeta">{event.decision_id ? `${copy(pageContract, "label.decision")} ${shortId(event.decision_id)}` : copy(pageContract, "label.no_decision")}</div>
                </div>
                <span className="tag t-mut">{humanizeEnum(event.actor_type)}</span>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </MuiCard>
  );

  return (
    <div className="kit-enter screen on gp-page">
      <div>
        <PageHeader
          title={goat.display_id}
          crumbs={[{ label: copy(pageContract, "fallback.title") }, { label: goat.display_id }]}
          actions={
            <div className="gp-status">
              <Tag tone={lifecycleTone(lifecycle)}>{humanizeEnum(lifecycle)}</Tag>
              {health ? <Tag tone={healthTone(health)}>{humanizeEnum(health)}</Tag> : null}
            </div>
          }
        />
      </div>

      <Notice status={actionStatus} actionKey={actionKey} pageContract={pageContract} />

      <div>{coverCard}</div>

      <div>
        {selectedTab === "" && summaryBody}
        {selectedTab === "identifiers" && identifiersBody}
        {selectedTab === "evidence" && evidenceBody}
        {selectedTab === "history" && historyBody}
      </div>
    </div>
  );
}

function EvidenceFields({ defaultType, defaultID, pageContract }: { defaultType: string; defaultID: string; pageContract: AdminUiPageContract }) {
  return (
    <>
      <FormSelect name="evidence_type" label={copy(pageContract, "field.evidence_type")} defaultValue={defaultType} options={optionGroup(pageContract, "evidence_types")} required emptyLabel={copy(pageContract, "label.select")} />
      <FormField name="evidence_id" label={copy(pageContract, "field.evidence_id")} defaultValue={defaultID} required />
      <FormField name="evidence_source_system" label={copy(pageContract, "field.evidence_source")} placeholder={copy(pageContract, "placeholder.optional")} />
    </>
  );
}
