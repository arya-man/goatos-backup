import Link from "next/link";
import { Workflow } from "lucide-react";
import { ConfigConsole, type ConfigRuleRow } from "./config-console";
import {
  hasSourceEvidenceFields,
  isPublishableSourceFields,
  type AnimalStageOption,
  type SopVersionOption,
} from "./rule-dsl";
import { listAnimalStages, listProtocolConfigs, listSops, type ProtocolConfigItem } from "@/lib/api/server";
import { control, copy, optionGroup, optionLabel, optionTone, type AdminUiPageContract } from "@/lib/admin-ui-contract";

// The generic CEO/COO authoring surface (obligation-engine §2.1 config-UI contract). One Config screen
// authors every protocol category; the engine, obligations, SOP tasks, and adherence all flow from
// PUBLISHED, source-backed rules. Field / verifier / park users never reach this screen — they only
// see generated obligations + SOP tasks.

function resolveCategory(category: string, pageContract: AdminUiPageContract): string {
  const categories = optionGroup(pageContract, "rule_categories");
  return categories.some((option) => option.key === category) ? category : categories[0]?.key ?? category;
}

function isPublishableSource(item: ProtocolConfigItem, pageContract: AdminUiPageContract): boolean {
  return isPublishableSourceFields({
    sourceSystem: item.source_system,
    sourceRef: item.source_ref,
    reviewStatus: item.review_status,
    approvedBy: item.approved_by,
    approvedAt: item.approved_at,
  }, optionGroup(pageContract, "source_systems"));
}

function fmtDate(iso: string | null | undefined, pageContract: AdminUiPageContract): string {
  return iso ? iso.slice(0, 10) : copy(pageContract, "label.placeholder");
}

function shortId(id: string | null | undefined, pageContract: AdminUiPageContract): string {
  if (!id) return copy(pageContract, "label.placeholder");
  return id.length > 10 ? `${id.slice(0, 8)}…` : id;
}

function scopeLabel(item: ProtocolConfigItem, pageContract: AdminUiPageContract): string {
  if (item.scope_label?.trim()) return item.scope_label;
  if (item.scope_type === "tenant") return copy(pageContract, "modal.rule_editor.label.tenant");
  if (item.scope_type === "park") {
    const parkScope = optionGroup(pageContract, "rule_scopes").find((option) => option.key === `park:${item.scope_id}`)?.label;
    return parkScope ?? `${copy(pageContract, "modal.rule_editor.label.park_scope_prefix")} ${shortId(item.scope_id, pageContract)}`;
  }
  return `${item.scope_type}: ${shortId(item.scope_id, pageContract)}`;
}

function hasSourceEvidence(item: ProtocolConfigItem, pageContract: AdminUiPageContract): boolean {
  return hasSourceEvidenceFields({
    sourceSystem: item.source_system,
    sourceRef: item.source_ref,
  }, optionGroup(pageContract, "source_systems"));
}

function statusOf(item: ProtocolConfigItem, pageContract: AdminUiPageContract): { text: string; tone: ConfigRuleRow["statusTone"] } {
  const key = item.status === "published"
    ? "published"
    : item.status === "retired"
      ? "retired"
      : isPublishableSource(item, pageContract)
        ? "draft_publishable"
        : hasSourceEvidence(item, pageContract)
          ? "draft_pending_approval"
          : "draft_not_publishable";
  return {
    text: optionLabel(pageContract, "protocol_rule_status", key),
    tone: optionTone(pageContract, "protocol_rule_status", key) as ConfigRuleRow["statusTone"],
  };
}

// Project a backend protocol-config version into a Config table row. No invented values: rule count,
// status, scope, effective date, linked SOP, and publisher are backend truth ("—" when absent).
function toRuleRow(item: ProtocolConfigItem, pageContract: AdminUiPageContract): ConfigRuleRow {
  const status = statusOf(item, pageContract);
  return {
    id: item.protocol_version_id,
    categoryLabel: item.name || item.code,
    ruleRows: item.rule_count,
    ruleRowLabel: item.rule_count === 1 ? copy(pageContract, "modal.rule_editor.label.rule_singular") : copy(pageContract, "modal.rule_editor.label.rule_plural"),
    version: item.version_label?.trim() || `v${item.version}`,
    scope: scopeLabel(item, pageContract),
    statusText: status.text,
    statusTone: status.tone,
    effective: fmtDate(item.effective_from, pageContract),
    linkedSop: shortId(item.sop_version_id, pageContract),
    lastPublisher: shortId(item.published_by, pageContract),
  };
}

// Shown at Admin / Data Ops / Config. This is the generic protocol authority surface: PHC/
// Vaccination links here with category=vaccination, but no module owns the screen. Rules are read
// from the real backend protocol list (B3, GET /protocols?category=…) through the generated client —
// never fabricated. A failed read surfaces an error band, not a silent empty table.
export async function ConfigProtocolRulesPage({ category, pageContract }: { category: string; pageContract: AdminUiPageContract }) {
  const initialCategory = resolveCategory(category, pageContract);
  const publishControl = control(pageContract, "publish_protocol_version");
  const [res, sopRes, stagesRes] = await Promise.all([
    listProtocolConfigs(initialCategory),
    listSops({ status: "active" }),
    listAnimalStages(),
  ]);
  const rules: ConfigRuleRow[] = res.ok ? res.data.items.map((item) => toRuleRow(item, pageContract)) : [];
  const loadError = res.ok ? null : (res.error.message ?? copy(pageContract, "error.rules_load"));

  // Real published SOP versions the author can bind to a protocol version. An active SOP exposes its
  // published version via active_sop_version_id; publish requires one (no hardcoded SOP labels). A
  // SUCCESSFUL empty list is honest (no published SOP yet); a FAILED read (403/500/backend-down) must
  // NOT masquerade as "no published SOP version" — it surfaces as an error band and blocks authoring.
  const sopVersions: SopVersionOption[] = sopRes.ok
    ? sopRes.data.items
        .filter((s) => s.status === "active" && !!s.active_sop_version_id)
        .map((s) => ({
          id: s.active_sop_version_id as string,
          label: `${s.name || s.code} · ${(s.active_sop_version_id as string).slice(0, 8)}…`,
        }))
    : [];
  const sopsError = sopRes.ok ? null : (sopRes.error.message ?? copy(pageContract, "error.sops_load"));

  // Backend-driven stage vocabulary (animal_stage_lookup). The Config stage picker uses these rows,
  // never hardcoded K0/K1/K2. A SUCCESSFUL empty list is honest (no stages seeded → seed-state); a
  // FAILED read (403/500/backend-down) must NOT masquerade as "no stages" — it surfaces as an error
  // band and blocks authoring, so an outage is never hidden as missing reference data.
  const animalStages: AnimalStageOption[] = stagesRes.ok
    ? stagesRes.data.items.map((s) => ({
        code: s.stage_code,
        label: s.name ? `${s.stage_code} · ${s.name}` : s.stage_code,
      }))
    : [];
  const stagesError = stagesRes.ok ? null : (stagesRes.error.message ?? copy(pageContract, "error.stages_load"));

  return (
    <div className="screen on">
      <ConfigConsole
        rules={rules}
        initialCategory={initialCategory}
        sopVersions={sopVersions}
        animalStages={animalStages}
        loadError={loadError}
        stagesError={stagesError}
        sopsError={sopsError}
        canPublish={publishControl.enabled}
        publishDisabledReason={publishControl.disabled_reason}
        pageContract={pageContract}
      />

      {/* How a published rule maps to live work */}
      <section className="card">
        <div className="hd">
          <Workflow className="ic" aria-hidden="true" />
          <h3>{copy(pageContract, "section.process_map.title")}</h3>
        </div>
        <div className="bd">
          <div
            className="mono"
            style={{
              background: "var(--bg)",
              border: "1px solid var(--line)",
              borderRadius: 9,
              padding: "11px 13px",
              fontSize: 12,
              color: "var(--muted)",
            }}
          >
            {copy(pageContract, "process_map.text")}
          </div>
          <div className="note" style={{ marginTop: 10 }}>
            {copy(pageContract, "process_map.note")}{" "}
            <Link href="/protocol-adherence" className="lk">
              {copy(pageContract, "action.open_adherence")}
            </Link>
          </div>
        </div>
      </section>
    </div>
  );
}
