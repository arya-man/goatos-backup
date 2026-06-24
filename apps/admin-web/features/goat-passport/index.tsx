import { randomUUID } from "node:crypto";
import { AlertTriangle, BadgeCheck, FileText, Fingerprint, History, Plus, ShieldCheck } from "lucide-react";
import { redirect } from "next/navigation";
import { ConfirmSubmitButton } from "@/components/confirm-submit-button";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { dateTime, dash, joinParts, shortId } from "@/lib/format";
import { Tag } from "@/components/ui-primitives";
import { firstAuthRequiredError, getGoatPassport, getGoatTimeline } from "@/lib/api/server";
import { hrefWithoutAction, one, type RouteSearchParams } from "@/lib/search-params";
import { addIdentifierAction, retireIdentifierAction } from "./actions";

const identifierTypes = ["old_tag", "rfid", "visual_tag", "sheet_row_id", "purchase_load_id", "temp_field_id", "external_system_id"];
const evidenceTypes = ["source_record", "identifier", "goat", "event", "media", "decision", "import_run", "conflict", "location", "actor"];

function MiniMetric({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div>
      <div className="k">{label}</div>
      <div className="v">{value}</div>
    </div>
  );
}

function Notice({ status, message }: { status?: string; message?: string }) {
  if (!status) return null;
  if (status === "success") {
    return (
      <div className="note" style={{ marginBottom: 14 }}>
        <Tag tone="ok">done</Tag> {message ?? "Action completed."}
      </div>
    );
  }
  return (
    <div className="alert" style={{ marginBottom: 14 }}>
      <AlertTriangle className="ic" aria-hidden="true" />
      <div>{message ?? status}</div>
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
  options: string[];
  required?: boolean;
  defaultValue?: string;
  emptyLabel?: string;
}) {
  return (
    <div>
      <label>{label}</label>
      <select name={name} aria-label={label} required={required} defaultValue={defaultValue ?? ""}>
        {emptyLabel ? <option value="">{emptyLabel}</option> : null}
        {options.map((option) => (
          <option key={option} value={option}>
            {option}
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

export async function GoatPassportPage({ goatId, searchParams = {} }: { goatId: string; searchParams?: RouteSearchParams }) {
  const [result, timeline] = await Promise.all([getGoatPassport(goatId), getGoatTimeline({ goatId, limit: 20 })]);
  const returnTo = hrefWithoutAction(`/goats/${encodeURIComponent(goatId)}`, searchParams);
  const actionStatus = one(searchParams, "action_status");
  const actionMessage = one(searchParams, "action_message");
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
              Passport · <b>Goat</b>
            </div>
            <h1>Goat Passport</h1>
            <div className="sub">Open a known goat detail URL or drill in from scoped operational work.</div>
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
            Passport · <b>Goat</b>
          </div>
          <h1>{goat.display_id}</h1>
          <div className="sub">Contextual identity passport with identifiers, evidence, row version, and merge state.</div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Tag tone="mut">row version {goat.row_version}</Tag>
      </div>

      <Notice status={actionStatus} message={actionMessage} />

      <div className="grid g2" style={{ marginBottom: 14 }}>
        <section className="card">
          <div className="hd">
            <BadgeCheck className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
            <h3>Summary</h3>
            <div className="sp" />
            <Tag tone={goat.identity_state === "clean" ? "ok" : "warn"}>{goat.identity_state}</Tag>
          </div>
          <div className="bd">
            <div className="metagrid" style={{ gridTemplateColumns: "repeat(3,minmax(0,1fr))" }}>
              <MiniMetric label="Goat ID" value={<span className="gid">{shortId(goat.goat_id)}</span>} />
              <MiniMetric label="RFID" value={dash(goat.summary.rfid)} />
              <MiniMetric label="Primary old tag" value={dash(goat.summary.primary_old_tag)} />
              <MiniMetric label="Breed / sex" value={joinParts([goat.summary.breed, goat.summary.sex])} />
              <MiniMetric label="Lifecycle" value={goat.summary.lifecycle_status} />
              <MiniMetric label="Health" value={dash(goat.summary.health_status)} />
              <MiniMetric label="Reproductive" value={dash(goat.summary.reproductive_status)} />
              <MiniMetric label="Growth cohort" value={dash(goat.summary.growth_cohort_tag)} />
              <MiniMetric label="Management" value={dash(goat.summary.management_stage)} />
            </div>
            <div className="note" style={{ marginTop: 12 }}>
              Location: <b>{dash(goat.summary.location_path.display)}</b>
              {goat.merged_into_goat_id ? (
                <>
                  {" "}
                  · merged into <span className="gid">{shortId(goat.merged_into_goat_id)}</span>
                </>
              ) : null}
            </div>
          </div>
        </section>

        <section className="card">
          <div className="hd">
            <ShieldCheck className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
            <h3>Warnings</h3>
            <div className="sp" />
            <Tag tone={allWarnings.length ? "warn" : "ok"}>{allWarnings.length}</Tag>
          </div>
          <div className="bd">
            {allWarnings.length === 0 ? (
              <p className="muted small" style={{ margin: 0 }}>
                No passport warnings returned.
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
            <h3>Identifiers</h3>
            <div className="sp" />
            <Tag tone="mut">{goat.identifiers.length}</Tag>
          </div>
          {goat.identifiers.length === 0 ? (
            <div className="bd">
              <p className="muted small">No identifiers returned for this passport.</p>
            </div>
          ) : (
            <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Identifiers">
              <table>
                <thead>
                  <tr>
                    <th>Type</th>
                    <th>Value</th>
                    <th>Scope</th>
                    <th>Status</th>
                    <th>Primary</th>
                    <th>Valid from</th>
                    <th>Action</th>
                  </tr>
                </thead>
                <tbody>
                  {goat.identifiers.map((identifier) => (
                    <tr key={identifier.identifier_id}>
                      <td>{identifier.identifier_type}</td>
                      <td className="mono">{identifier.identifier_value}</td>
                      <td>{identifier.scope_key}</td>
                      <td>
                        <Tag tone={identifier.status === "active" ? "ok" : "mut"}>{identifier.status}</Tag>
                      </td>
                      <td>{identifier.is_primary_for_goat ? "yes" : "no"}</td>
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
                            <input type="hidden" name="reason" value="Retired from passport identifier review." />
                            <ConfirmSubmitButton message={`Retire ${identifier.identifier_type} ${identifier.identifier_value}?`} className="btn sm">
                              Retire
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
                <h3>Add identifier</h3>
              </div>
              <div className="grid g4">
                <FormSelect name="identifier_type" label="Identifier type" options={identifierTypes} required emptyLabel="Select" />
                <FormField name="identifier_value" label="Identifier value" required />
                <FormField name="scope_key" label="Scope key" required placeholder="global or park scope" />
                <label style={{ display: "flex", alignItems: "center", gap: 8, marginTop: 24 }}>
                  <input name="is_primary_for_goat" type="checkbox" style={{ width: "auto" }} />
                  Primary
                </label>
              </div>
              <div className="grid g3" style={{ marginTop: 8 }}>
                <EvidenceFields defaultType="goat" defaultID={goat.goat_id} />
              </div>
              <div style={{ display: "flex", justifyContent: "flex-end", marginTop: 12 }}>
                <button className="btn p">Add identifier</button>
              </div>
            </form>
          </div>
        </section>

        <section className="card">
          <div className="hd">
            <FileText className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
            <h3>Evidence</h3>
            <div className="sp" />
            <Tag tone="mut">{goat.evidence_refs.length}</Tag>
          </div>
          <div className="bd">
            {goat.evidence_refs.length === 0 ? (
              <p className="muted small" style={{ margin: 0 }}>
                No evidence refs returned.
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
          <h3>Timeline</h3>
          <div className="sp" />
          <span className="muted small">goat_identity_events</span>
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
              No identity events returned for this goat.
            </p>
          ) : (
            <div className="htl">
              {timeline.data.items.map((event) => (
                <div key={event.event_id} className="hrow">
                  <span className="hdot t-info" />
                  <div className="htx">
                    <b>{event.event_type}</b>
                    <div className="hmeta">
                      occurred {dateTime(event.occurred_at)} · recorded {dateTime(event.recorded_at)} · evidence{" "}
                      {event.evidence_refs.length}
                    </div>
                    <div className="hmeta">{event.decision_id ? `decision ${shortId(event.decision_id)}` : "no decision"}</div>
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

function EvidenceFields({ defaultType, defaultID }: { defaultType: string; defaultID: string }) {
  return (
    <>
      <FormSelect name="evidence_type" label="Evidence type" defaultValue={defaultType} options={evidenceTypes} required emptyLabel="Select" />
      <FormField name="evidence_id" label="Evidence ID" defaultValue={defaultID} required />
      <FormField name="evidence_source_system" label="Evidence source" placeholder="optional" />
    </>
  );
}
