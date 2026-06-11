import { BadgeCheck } from "lucide-react";
import { EmptyPanel, ErrorPanel, Mono, PageHeader, Panel, StatPill, ValueList } from "@/components/admin-primitives";
import { dateTime, dash, joinParts, shortId } from "@/lib/format";
import { getGoatPassport } from "@/lib/api/server";

export async function GoatPassportPage({ goatId }: { goatId: string }) {
  const result = await getGoatPassport(goatId);
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
        <Panel title="Identifiers">
          {goat.identifiers.length === 0 ? (
            <EmptyPanel message="No identifiers returned for this passport." />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[760px] text-left text-sm">
                <thead className="text-xs uppercase text-[#93a4b8]">
                  <tr className="border-b border-[#293241]">
                    <th className="px-2 py-2">Type</th>
                    <th className="px-2 py-2">Value</th>
                    <th className="px-2 py-2">Scope</th>
                    <th className="px-2 py-2">Status</th>
                    <th className="px-2 py-2">Primary</th>
                    <th className="px-2 py-2">Valid from</th>
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
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
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
        <Panel title="Timeline" description="Timeline is not tracked in this screen yet.">
          <EmptyPanel message="Timeline will appear here once that read view is available." />
        </Panel>
      </div>
    </>
  );
}
