import { VaccinationPlanConsole } from "@/features/vaccination-plan";
import { groupSchedule } from "@/features/vaccination-plan/plan-model";
import { getProtocolVersion, listProtocolConfigs, requireAdminWebPageContract } from "@/lib/api/server";

export const dynamic = "force-dynamic";

/**
 * Preventive Care / Vaccination plan.
 *
 * Replaces Admin / Data Ops / Config for the vaccination category, and absorbs the
 * former /vaccination/sops surface. The plan is vaccination-only and the CEO/COO who
 * publishes it works out of Preventive Care, so it lives here.
 *
 * Reads are server-side and scoped: the version list plus the one live version's
 * document, never a full obligation scan (admin-web-request-reads-guard).
 */
export default async function Page() {
  const [, configs] = await Promise.all([
    requireAdminWebPageContract("vaccination-plan"),
    listProtocolConfigs("vaccination"),
  ]);

  const versions = configs.ok ? (configs.data.items ?? []) : [];
  const live = versions.find((item) => item.status === "published");

  // The vaccine table is a read of the live plan's own document. It is fetched
  // only when a live version exists, so a fresh tenant costs one call, not two.
  const liveDoc = live ? await getProtocolVersion(live.protocol_version_id) : null;
  const liveVaccines = liveDoc?.ok ? groupSchedule(liveDoc.data.rule_dsl) : [];

  return (
    <VaccinationPlanConsole
      versions={versions}
      liveVaccines={liveVaccines}
      loadError={configs.ok ? null : "The vaccination plan could not be loaded."}
    />
  );
}
