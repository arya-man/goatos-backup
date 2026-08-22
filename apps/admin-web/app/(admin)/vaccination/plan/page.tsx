import { describeChange, readVaccines, VaccinationPlanConsole, type VaccineGroup } from "@/features/vaccination-plan";
import { getProtocolVersion, listProtocolConfigs, requireAdminWebPageContract } from "@/lib/api/server";

export const dynamic = "force-dynamic";

/**
 * How many versions' documents are read to build the catalog and the change
 * notes. History grows without bound, the screen shows a handful, and an
 * unbounded per-version fetch is exactly what admin-web-request-reads-guard
 * exists to stop. Anything older is still listed -- it just has no derived
 * "what changed" line, which is honest rather than invented.
 */
const HISTORY_DEPTH = 12;

/**
 * Preventive Care / Vaccination plan.
 *
 * Replaces Admin / Data Ops / Config for the vaccination category, and absorbs the
 * former /vaccination/sops surface. The plan is vaccination-only and the CEO/COO who
 * publishes it works out of Preventive Care, so it lives here.
 */
export default async function Page() {
  const [, configs] = await Promise.all([
    requireAdminWebPageContract("vaccination-plan"),
    listProtocolConfigs("vaccination"),
  ]);

  const versions = configs.ok ? (configs.data.items ?? []) : [];

  // Newest first, so "the version before this one" is the next element.
  const ordered = [...versions].sort((a, b) => (b.version ?? 0) - (a.version ?? 0));
  const recent = ordered.slice(0, HISTORY_DEPTH);

  const documents = await Promise.all(
    recent.map(async (item) => {
      const doc = await getProtocolVersion(item.protocol_version_id);
      return {
        versionId: item.protocol_version_id,
        groups: doc.ok ? readVaccines(doc.data.rule_dsl) : ([] as VaccineGroup[]),
        loaded: doc.ok,
      };
    }),
  );

  const groupsById = new Map(documents.map((d) => [d.versionId, d]));
  const live = versions.find((item) => item.status === "published");
  const catalog = live ? (groupsById.get(live.protocol_version_id)?.groups ?? []) : [];

  // A version's note compares it with the one immediately before it. Only
  // computed where BOTH documents were actually read -- a failed fetch must not
  // masquerade as "dropped every vaccine".
  const changeNotes: Record<string, string> = {};
  for (let i = 0; i < recent.length; i += 1) {
    const current = groupsById.get(recent[i].protocol_version_id);
    if (!current?.loaded) continue;
    const olderItem = recent[i + 1];
    if (!olderItem) {
      // Oldest one we read. It is only truly "the first plan" if it is also the
      // oldest that exists; otherwise there is nothing to compare against.
      if (recent.length === ordered.length) changeNotes[recent[i].protocol_version_id] = describeChange(current.groups, null);
      continue;
    }
    const older = groupsById.get(olderItem.protocol_version_id);
    if (!older?.loaded) continue;
    changeNotes[recent[i].protocol_version_id] = describeChange(current.groups, older.groups);
  }

  return (
    <VaccinationPlanConsole
      versions={versions}
      catalog={catalog}
      changeNotes={changeNotes}
      loadError={configs.ok ? null : "The vaccination plan could not be loaded."}
    />
  );
}
