import { randomUUID } from "node:crypto";
import { BadgeCheck } from "lucide-react";
import { redirect } from "next/navigation";
import { ActionNotice, EmptyPanel, ErrorPanel, FormField, FormSelect, Mono, PageHeader, Panel, StatPill, ValueList } from "@/components/admin-primitives";
import { ConfirmSubmitButton } from "@/components/confirm-submit-button";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { dateTime, dash, joinParts, shortId } from "@/lib/format";
import { firstAuthRequiredError, getGoatPassport, getGoatTimeline } from "@/lib/api/server";
import { hrefWithoutAction, one, type RouteSearchParams } from "@/lib/search-params";
import { addIdentifierAction, retireIdentifierAction } from "./actions";

const identifierTypes = ["old_tag", "rfid", "visual_tag", "sheet_row_id", "purchase_load_id", "temp_field_id", "external_system_id"];
const evidenceTypes = ["source_record", "identifier", "goat", "event", "media", "decision", "import_run", "conflict", "location", "actor"];

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
      <>
        <PageHeader eyebrow="Passport" title="Goat Passport" description="Goat passport detail is fetched by canonical goat_id from search results." />
        <ErrorPanel error={result.error} />
      </>
    );
  }

  const { goat, warnings } = result.data;
  return (
    <>
      <PageHeader
        eyebrow="Passport"
        title={goat.display_id}
        description="Read-only identity passport with identifiers, evidence, row version, and merge state."
        actions={<StatPill label="Row version" value={goat.row_version} />}
      />
      <ActionNotice status={actionStatus} message={actionMessage} />
      <div className="grid gap-5 xl:grid-cols-[1fr_0.9fr]">
        <Panel title="Summary" action={<BadgeCheck className="h-5 w-5 text-[#14f1d9]" aria-hidden="true" />}>
          <ValueList
            values={[
              ["Goat ID", <Mono key="id">{goat.goat_id}</Mono>],
              ["Species", goat.species],
              ["Identity state", goat.identity_state],
              ["Merged into", goat.merged_into_goat_id ? <Mono key="merged">{goat.merged_into_goat_id}</Mono> : "—"],
              ["RFID", dash(goat.summary.rfid)],
              ["Primary old tag", dash(goat.summary.primary_old_tag)],
              ["Breed / sex", joinParts([goat.summary.breed, goat.summary.sex])],
              ["Lifecycle", goat.summary.lifecycle_status],
              ["Reproductive", dash(goat.summary.reproductive_status)],
              ["Growth cohort", dash(goat.summary.growth_cohort_tag)],
              ["Management", dash(goat.summary.management_stage)],
              ["Health", dash(goat.summary.health_status)],
              ["Location", dash(goat.summary.location_path.display)],
            ]}
          />
        </Panel>
        <Panel title="Warnings">
          {warnings.length === 0 && goat.summary.warnings.length === 0 ? (
            <EmptyPanel message="No passport warnings returned." />
          ) : (
            <div className="space-y-2">
              {[...warnings, ...goat.summary.warnings].map((warning, index) => (
                <div key={`${warning.code}-${index}`} className="rounded-md border border-[#a16207] bg-[#1b1710] p-3 text-sm text-[#facc15]">
                  <div className="font-semibold">{warning.code}</div>
                  <p className="mt-1 text-[#f8dda1]">{warning.message}</p>
                  {warning.original_goat_id || warning.redirect_goat_id ? (
                    <p className="mt-2 font-mono text-xs">
                      {shortId(warning.original_goat_id)} → {shortId(warning.redirect_goat_id)}
                    </p>
                  ) : null}
                </div>
              ))}
            </div>
          )}
        </Panel>
      </div>

      <div className="mt-5 grid gap-5 xl:grid-cols-[1.2fr_0.8fr]">
        <Panel title="Identifiers" description="Attach or retire identifiers through the defined Phase 1 identity write service.">
          {goat.identifiers.length === 0 ? (
            <EmptyPanel message="No identifiers returned for this passport." />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[940px] text-left text-sm">
                <thead className="text-xs uppercase text-[#93a4b8]">
                  <tr className="border-b border-[#293241]">
                    <th className="px-2 py-2">Type</th>
                    <th className="px-2 py-2">Value</th>
                    <th className="px-2 py-2">Scope</th>
                    <th className="px-2 py-2">Status</th>
                    <th className="px-2 py-2">Primary</th>
                    <th className="px-2 py-2">Valid from</th>
                    <th className="px-2 py-2">Action</th>
                  </tr>
                </thead>
                <tbody>
                  {goat.identifiers.map((identifier) => (
                    <tr key={identifier.identifier_id} className="border-b border-[#293241]">
                      <td className="px-2 py-2">{identifier.identifier_type}</td>
                      <td className="px-2 py-2 font-mono text-xs">{identifier.identifier_value}</td>
                      <td className="px-2 py-2">{identifier.scope_key}</td>
                      <td className="px-2 py-2">{identifier.status}</td>
                      <td className="px-2 py-2">{identifier.is_primary_for_goat ? "yes" : "no"}</td>
                      <td className="px-2 py-2">{dateTime(identifier.valid_from)}</td>
                      <td className="px-2 py-2">
                        {identifier.status === "active" ? (
                          <form action={retireIdentifierAction} className="flex flex-wrap items-end gap-2">
                            <input type="hidden" name="goat_id" value={goat.goat_id} />
                            <input type="hidden" name="identifier_id" value={identifier.identifier_id} />
                            <input type="hidden" name="row_version" value={goat.row_version} />
                            <input type="hidden" name="idempotency_key" value={randomUUID()} />
                            <input type="hidden" name="return_to" value={returnTo} />
                            <input type="hidden" name="evidence_type" value="identifier" />
                            <input type="hidden" name="evidence_id" value={identifier.identifier_id} />
                            <input type="hidden" name="reason" value="Retired from passport identifier review." />
                            <ConfirmSubmitButton
                              message={`Retire ${identifier.identifier_type} ${identifier.identifier_value}?`}
                              className="h-10 rounded-md border border-[#7f1d1d] px-3 text-xs font-semibold text-[#fecaca] hover:bg-[#1d1214]"
                            >
                              Retire
                            </ConfirmSubmitButton>
                          </form>
                        ) : (
                          "—"
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <form action={addIdentifierAction} className="mt-5 rounded-md border border-[#334155] bg-[#10141b] p-3">
            <input type="hidden" name="goat_id" value={goat.goat_id} />
            <input type="hidden" name="row_version" value={goat.row_version} />
            <input type="hidden" name="idempotency_key" value={randomUUID()} />
            <input type="hidden" name="return_to" value={returnTo} />
            <div className="grid gap-3 md:grid-cols-4">
              <FormSelect name="identifier_type" label="Identifier type" options={identifierTypes} required emptyLabel="Select" />
              <FormField name="identifier_value" label="Identifier value" required />
              <FormField name="scope_key" label="Scope key" required placeholder="global or park scope" />
              <label className="flex min-h-10 items-center gap-2 text-sm text-[#c7d1dc]">
                <input name="is_primary_for_goat" type="checkbox" className="h-4 w-4 accent-[#14f1d9]" />
                Primary
              </label>
            </div>
            <div className="mt-3">
              <EvidenceFields defaultType="goat" defaultID={goat.goat_id} />
            </div>
            <div className="mt-3 flex justify-end">
              <button className="h-10 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]">Add identifier</button>
            </div>
          </form>
        </Panel>
        <Panel title="Evidence">
          {goat.evidence_refs.length === 0 ? (
            <EmptyPanel message="No evidence refs returned." />
          ) : (
            <div className="space-y-2">
              {goat.evidence_refs.map((evidence) => (
                <div key={`${evidence.evidence_type}-${evidence.evidence_id}`} className="rounded-md border border-[#293241] bg-[#10141b] p-3 text-sm">
                  <div className="font-semibold text-white">{evidence.evidence_type}</div>
                  <Mono>{evidence.evidence_id}</Mono>
                  <div className="mt-1 text-[#93a4b8]">{dash(evidence.description ?? evidence.source_system)}</div>
                </div>
              ))}
            </div>
          )}
        </Panel>
      </div>

      <div className="mt-5">
        <Panel title="Timeline" description="Live identity events from goat_identity_events. Audit-log enrichment is deferred.">
          {!timeline.ok ? (
            <ErrorPanel error={timeline.error} />
          ) : timeline.data.items.length === 0 ? (
            <EmptyPanel message="No identity events returned for this goat." />
          ) : (
            <div className="space-y-3">
              {timeline.data.items.map((event) => (
                <div key={event.event_id} className="rounded-md border border-[#293241] bg-[#10141b] p-3 text-sm">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div>
                      <div className="font-semibold text-white">{event.event_type}</div>
                      <div className="mt-1 text-[#93a4b8]">occurred {dateTime(event.occurred_at)}</div>
                    </div>
                    <span className="rounded border border-[#334155] px-2 py-1 text-xs text-[#c7d1dc]">{event.actor_type}</span>
                  </div>
                  <div className="mt-3 grid gap-1 text-xs text-[#c7d1dc] sm:grid-cols-3">
                    <span>recorded {dateTime(event.recorded_at)}</span>
                    <span>evidence {event.evidence_refs.length}</span>
                    <span>{event.decision_id ? `decision ${shortId(event.decision_id)}` : "no decision"}</span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Panel>
      </div>
    </>
  );
}

function EvidenceFields({ defaultType, defaultID }: { defaultType: string; defaultID: string }) {
  return (
    <div className="grid gap-3 sm:grid-cols-3">
      <FormSelect name="evidence_type" label="Evidence type" defaultValue={defaultType} options={evidenceTypes} required emptyLabel="Select" />
      <FormField name="evidence_id" label="Evidence ID" defaultValue={defaultID} required />
      <FormField name="evidence_source_system" label="Evidence source" placeholder="optional" />
    </div>
  );
}
