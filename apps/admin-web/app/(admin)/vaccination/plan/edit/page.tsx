import { notFound, redirect } from "next/navigation";

import { fromRuleDsl, VaccinationPlanEditor } from "@/features/vaccination-plan";
import {
  getProtocolVersion,
  listProtocolConfigs,
  previewVaccinationImpact,
  requireAdminWebPageContract,
} from "@/lib/api/server";
import { control, controlEnabled } from "@/lib/admin-ui-contract";
import { one, type RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

/**
 * Preventive Care / Vaccination plan / edit a draft.
 *
 * A draft only. A published version is immutable in the database, so an editor
 * pointed at one could not save and must not pretend otherwise. A missing
 * version parameter is still malformed, but stale draft bookmarks recover to
 * the plan console because saves and publishes replace/retire draft rows.
 */
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const params = await searchParams;
  const versionId = one(params, "version");
  if (!versionId) notFound();

  const [contract, configs, version] = await Promise.all([
    requireAdminWebPageContract("vaccination-plan"),
    listProtocolConfigs("vaccination"),
    getProtocolVersion(versionId),
  ]);

  if (!version.ok || version.data.status !== "draft") redirect("/vaccination/plan");

  // Sized against the current herd by the backend's own rollup. A failure here
  // hides the card rather than showing a zero, which would read as "no effect".
  const preview = await previewVaccinationImpact({});
  const impact = preview.ok
    ? {
        eligibleAnimals: preview.data.eligible_animals,
        affectedSheds: preview.data.affected_sheds,
        estimatedDays: preview.data.estimated_days,
        dailyCap: preview.data.daily_cap,
        capacityStatus: preview.data.capacity_status,
      }
    : null;

  const items = configs.ok ? (configs.data.items ?? []) : [];
  const draft = items.find((i) => i.protocol_version_id === versionId);
  const live = items.find((i) => i.status === "published");

  return (
    <VaccinationPlanEditor
      protocolId={version.data.protocol_id}
      draftVersionId={versionId}
      draftLabel={draft?.version_label || `V${version.data.version}`}
      liveLabel={live?.version_label ?? null}
      liveSince={formatDate(live?.effective_from)}
      scopeType={version.data.scope_type}
      originalRuleDsl={version.data.rule_dsl}
      proofPolicy={version.data.proof_policy}
      initialPlan={fromRuleDsl(version.data.rule_dsl, version.data.proof_policy)}
      impact={impact}
      // The publish gate is the backend's, not this screen's. The header says
      // "only you and the COO can publish", and that was decoration: the button
      // rendered and worked for anyone who could open the page. The backend
      // already emits a publish_protocol_version control carrying whether this
      // principal holds protocol.publish, and why not.
      canPublish={controlEnabled(contract, "publish_protocol_version", true)}
      cannotPublishReason={control(contract, "publish_protocol_version").disabled_reason || null}
    />
  );
}

function formatDate(value: string | undefined | null): string | null {
  if (!value) return null;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return null;
  return date.toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" });
}
