import { randomUUID } from "node:crypto";
import { AlertTriangle, BadgeCheck, FileText, Fingerprint, History, Plus, ShieldCheck } from "lucide-react";
import { redirect } from "next/navigation";
import { ConfirmSubmitButton } from "@/components/confirm-submit-button";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { dateTime, dash, joinParts, shortId } from "@/lib/format";
import { Tag } from "@/components/ui-primitives";
import { firstAuthRequiredError, getGoatPassport, getGoatTimeline } from "@/lib/api/server";
import { hrefWithoutAction, one, type RouteSearchParams } from "@/lib/search-params";
import { actionFeedbackCopy, copy, optionGroup, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { addIdentifierAction, retireIdentifierAction } from "./actions";

// Business-friendly labels for the two physical tag identifier types in the identifiers detail table.
// Other stored identifier types (mirrors, ear tags, vendor tags) keep their raw type string.
function identifierTypeLabel(type: string, pageContract: AdminUiPageContract): string {
  if (type === "animal_identifier_1") return copy(pageContract, "label.tag_1");
  if (type === "animal_identifier_2") return copy(pageContract, "label.tag_2");
  return type;
}

function MiniMetric({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="fld">
      <div className="k">{label}</div>
      <div className="v">{value}</div>
    </div>
  );
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
    <div className="alert" style={{ marginBottom: 14 }}>
      <AlertTriangle className="ic" aria-hidden="true" />
      <div>{actionFeedbackCopy(pageContract, status, actionKey)}</div>
    </div>
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
    <div className="fld">
      <label>{label}</label>
      <select name={name} aria-label={label} required={required} defaultValue={defaultValue ?? ""}>
        {emptyLabel ? <option value="">{emptyLabel}</option> : null}
        {options.map((option) => (
          <option key={option.key} value={option.key}>
            {option.label}
          </option>
        ))}
      </select>
    </div>
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
    <div>
      <label>{label}</label>
      <input name={name} aria-label={label} required={required} defaultValue={defaultValue} placeholder={placeholder} />
    </div>
  );
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
        <div className="phead">
          <div>
            <div className="crumb">
	              {copy(pageContract, "fallback.title")}
	            </div>
	            <h1>{pageContract.title || copy(pageContract, "fallback.title")}</h1>
	            <div className="sub">{pageContract.subtitle}</div>
          </div>
        </div>
        <div className="alert">
          <AlertTriangle className="ic" aria-hidden="true" />
          <div>
            <b>{result.error.code}</b>
            <div className="muted small" style={{ marginTop: 4 }}>
              {result.error.message}
            </div>
          </div>
        </div>
      </div>
    );
  }

  const { goat, warnings } = result.data;
  const allWarnings = [...warnings, ...goat.summary.warnings];

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
	            {copy(pageContract, "fallback.title")}
          </div>
          <h1>{goat.display_id}</h1>
	          <div className="sub">{pageContract.subtitle}</div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Tag tone="mut">{copy(pageContract, "label.row_version")} {goat.row_version}</Tag>
      </div>

      <Notice status={actionStatus} actionKey={actionKey} pageContract={pageContract} />

      <div className="grid g2" style={{ marginBottom: 14 }}>
        <section className="card">
          <div className="hd">
            <BadgeCheck className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
	            <h3>{copy(pageContract, "section.summary.title")}</h3>
            <div className="sp" />
          </div>
          <div className="bd">
            <div className="metagrid" style={{ gridTemplateColumns: "repeat(3,minmax(0,1fr))" }}>
              <MiniMetric label={copy(pageContract, "label.display_id")} value={<span className="gid">{goat.display_id}</span>} />
              <MiniMetric label={copy(pageContract, "label.tag_1")} value={<span className="mono">{dash(goat.summary.animal_identifier_1)}</span>} />
              <MiniMetric label={copy(pageContract, "label.tag_2")} value={<span className="mono">{dash(goat.summary.animal_identifier_2)}</span>} />
              <MiniMetric label={copy(pageContract, "label.breed_sex")} value={joinParts([goat.summary.breed, goat.summary.sex])} />
              <MiniMetric label={copy(pageContract, "label.lifecycle")} value={goat.summary.lifecycle_status} />
              <MiniMetric label={copy(pageContract, "label.health")} value={dash(goat.summary.health_status)} />
              <MiniMetric label={copy(pageContract, "label.reproductive")} value={dash(goat.summary.reproductive_status)} />
              <MiniMetric label={copy(pageContract, "label.growth_cohort")} value={dash(goat.summary.growth_cohort_tag)} />
              <MiniMetric label={copy(pageContract, "label.management")} value={dash(goat.summary.management_stage)} />
            </div>
            <div className="note" style={{ marginTop: 12 }}>
              {copy(pageContract, "label.location")}: <b>{dash(goat.summary.location_path.operational_location_display)}</b>
              {goat.merged_into_goat_id ? (
                <>
                  {" "}
                  · {copy(pageContract, "label.merged_into")} <span className="gid">{shortId(goat.merged_into_goat_id)}</span>
                </>
              ) : null}
            </div>
          </div>
        </section>

        <section className="card">
          <div className="hd">
            <ShieldCheck className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
	            <h3>{copy(pageContract, "section.warnings.title")}</h3>
            <div className="sp" />
            <Tag tone={allWarnings.length ? "warn" : "ok"}>{allWarnings.length}</Tag>
          </div>
          <div className="bd">
            {allWarnings.length === 0 ? (
              <p className="muted small" style={{ margin: 0 }}>
                {copy(pageContract, "empty.warnings")}
              </p>
            ) : (
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
            )}
          </div>
        </section>
      </div>

      <div className="grid g2" style={{ marginBottom: 14 }}>
        <section className="card">
          <div className="hd">
            <Fingerprint className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
	            <h3>{copy(pageContract, "section.identifiers.title")}</h3>
            <div className="sp" />
            <Tag tone="mut">{goat.identifiers.length}</Tag>
          </div>
          {goat.identifiers.length === 0 ? (
            <div className="bd">
              <p className="muted small">{copy(pageContract, "empty.identifiers")}</p>
            </div>
          ) : (
            <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "table.identifiers.aria")}>
              <table>
                <thead>
                  <tr>
                    <th>{copy(pageContract, "label.type")}</th>
                    <th>{copy(pageContract, "label.value")}</th>
                    <th>{copy(pageContract, "label.scope")}</th>
                    <th>{copy(pageContract, "label.status")}</th>
                    <th>{copy(pageContract, "label.primary")}</th>
                    <th>{copy(pageContract, "label.valid_from")}</th>
                    <th>{copy(pageContract, "label.action")}</th>
                  </tr>
                </thead>
                <tbody>
                  {goat.identifiers.map((identifier) => (
                    <tr key={identifier.identifier_id}>
                      <td>{identifierTypeLabel(identifier.identifier_type, pageContract)}</td>
                      <td className="mono">{identifier.identifier_value}</td>
                      <td>{identifier.scope_key}</td>
                      <td>
                        <Tag tone={identifier.status === "active" ? "ok" : "mut"}>{identifier.status}</Tag>
                      </td>
                      <td>{identifier.is_primary_for_goat ? copy(pageContract, "label.yes") : copy(pageContract, "label.no")}</td>
                      <td>{dateTime(identifier.valid_from)}</td>
                      <td>
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
                            <ConfirmSubmitButton message={`${copy(pageContract, "confirm.retire_identifier")} ${identifier.identifier_type} ${identifier.identifier_value}`} className="btn sm">
                              {copy(pageContract, "action.retire_identifier")}
                            </ConfirmSubmitButton>
                          </form>
                        ) : (
                          <span className="muted small">-</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          <div className="bd" style={{ borderTop: "1px solid var(--line2)" }}>
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
                <label style={{ display: "flex", alignItems: "center", gap: 8, marginTop: 24 }}>
                  <input name="is_primary_for_goat" type="checkbox" style={{ width: "auto" }} />
                  {copy(pageContract, "label.primary")}
                </label>
              </div>
              <div className="grid g3" style={{ marginTop: 8 }}>
                <EvidenceFields defaultType="goat" defaultID={goat.goat_id} pageContract={pageContract} />
              </div>
              <div style={{ display: "flex", justifyContent: "flex-end", marginTop: 12 }}>
	                <button className="btn p">{copy(pageContract, "action.add_identifier")}</button>
              </div>
            </form>
          </div>
        </section>

        <section className="card">
          <div className="hd">
            <FileText className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
	            <h3>{copy(pageContract, "section.evidence.title")}</h3>
            <div className="sp" />
            <Tag tone="mut">{goat.evidence_refs.length}</Tag>
          </div>
          <div className="bd">
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
          </div>
        </section>
      </div>

      <section className="card">
        <div className="hd">
          <History className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.timeline.title")}</h3>
          <div className="sp" />
          <span className="muted small">{copy(pageContract, "label.identity_events_table")}</span>
        </div>
        <div className="bd">
          {!timeline.ok ? (
            <div className="alert">
              <AlertTriangle className="ic" aria-hidden="true" />
              <div>
                <b>{timeline.error.code}</b>
                <div className="muted small" style={{ marginTop: 4 }}>
                  {timeline.error.message}
                </div>
              </div>
            </div>
          ) : timeline.data.items.length === 0 ? (
            <p className="muted small" style={{ margin: 0 }}>
              {copy(pageContract, "empty.timeline")}
            </p>
          ) : (
            <div className="htl">
              {timeline.data.items.map((event) => (
                <div key={event.event_id} className="hrow">
                  <span className="hdot t-info" />
                  <div className="htx">
                    <b>{event.event_type}</b>
                    <div className="hmeta">
                      {copy(pageContract, "label.occurred")} {dateTime(event.occurred_at)} · {copy(pageContract, "label.recorded")} {dateTime(event.recorded_at)} · {copy(pageContract, "label.evidence")}{" "}
                      {event.evidence_refs.length}
                    </div>
                    <div className="hmeta">{event.decision_id ? `${copy(pageContract, "label.decision")} ${shortId(event.decision_id)}` : copy(pageContract, "label.no_decision")}</div>
                  </div>
                  <span className="tag t-mut">{event.actor_type}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      </section>
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
