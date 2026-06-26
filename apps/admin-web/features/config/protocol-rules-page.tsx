import Link from "next/link";
import { AlertTriangle, Workflow } from "lucide-react";
import { ConfigConsole, type ConfigRuleRow } from "./config-console";
import { CATEGORIES } from "./rule-dsl";
import { listProtocolConfigs, type ProtocolConfigItem } from "@/lib/api/server";

// The generic CEO/COO authoring surface (obligation-engine §2.1 config-UI contract). One Config screen
// authors every protocol category; the engine, obligations, SOP tasks, and adherence all flow from
// PUBLISHED, source-backed rules. Field / verifier / park users never reach this screen — they only
// see generated obligations + SOP tasks.

function resolveCategory(category: string): string {
  return CATEGORIES.includes(category) ? category : "vaccination";
}

// Mirrors the backend publish gate (protocol/app/publish.go publishableSources). A draft is
// publishable ONLY when source-backed, reviewed, and approved; otherwise the row says so plainly.
const PUBLISHABLE_SOURCES = new Set(["vaccinations_db", "phc", "vet"]);

function isPublishableSource(item: ProtocolConfigItem): boolean {
  return (
    PUBLISHABLE_SOURCES.has(item.source_system) &&
    item.source_ref.trim() !== "" &&
    item.review_status === "approved" &&
    item.approved_by.trim() !== ""
  );
}

function fmtDate(iso?: string | null): string {
  return iso ? iso.slice(0, 10) : "—";
}

function shortId(id?: string): string {
  if (!id) return "—";
  return id.length > 10 ? `${id.slice(0, 8)}…` : id;
}

function statusOf(item: ProtocolConfigItem): { text: string; tone: ConfigRuleRow["statusTone"] } {
  if (item.status === "published") return { text: "Published", tone: "ok" };
  if (item.status === "retired") return { text: "Retired", tone: "mut" };
  return isPublishableSource(item)
    ? { text: "Draft · source-backed", tone: "info" }
    : { text: "Draft · not source-backed", tone: "warn" };
}

// Project a backend protocol-config version into a Config table row. No invented values: rule count,
// status, scope, effective date, linked SOP, and publisher are backend truth ("—" when absent).
function toRuleRow(item: ProtocolConfigItem): ConfigRuleRow {
  const status = statusOf(item);
  return {
    id: item.protocol_version_id,
    categoryLabel: item.name || item.code,
    ruleRows: item.rule_count,
    ruleRowLabel: item.rule_count === 1 ? "rule" : "rules",
    version: item.version_label?.trim() || `v${item.version}`,
    scope: item.scope_type === "tenant" ? "tenant" : `park:${item.scope_id ?? ""}`,
    statusText: status.text,
    statusTone: status.tone,
    effective: fmtDate(item.effective_from),
    linkedSop: shortId(item.sop_version_id),
    lastPublisher: shortId(item.published_by),
  };
}

// Rendered at Admin / Data Ops / Config. This is the generic protocol authority surface: PHC/
// Vaccination links here with category=vaccination, but no module owns the screen. Rules are read
// from the real backend protocol list (B3, GET /protocols?category=…) through the generated client —
// never fabricated. A failed read surfaces an error band, not a silent empty table.
export async function ConfigProtocolRulesPage({ category }: { category: string }) {
  const initialCategory = resolveCategory(category);
  const res = await listProtocolConfigs(initialCategory);
  const rules: ConfigRuleRow[] = res.ok ? res.data.items.map(toRuleRow) : [];
  const loadError = res.ok ? null : (res.error.message ?? "could not load protocol rules");

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            Admin · Data Ops · <b>Config</b>
          </div>
          <h1>Config — Protocol Rules</h1>
          <div className="sub">
            <b>What should happen.</b> CEO/COO author + publish the business/medical config. Obligations, SOP tasks
            &amp; adherence gaps all flow from <b>published</b>, source-backed rules across categories.
          </div>
        </div>
      </div>

      <div className="alert warn" style={{ marginBottom: 14 }}>
        <AlertTriangle className="ic" aria-hidden="true" />
        <div>
          Real business/medical config — <b>not public</b>. Only <b>CEO/COO publish</b> · Directors draft/propose if
          granted the capability · <b>field / verifier / park never see raw config</b> (they get generated obligations
          + SOP tasks only). The category dropdown drives the form and <span className="mono">rule_dsl</span>; no values
          are invented, and a draft publishes only when source-backed, reviewed, and approved.
        </div>
      </div>

      {loadError ? (
        <div className="alert" role="alert" style={{ marginBottom: 14 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div>
            Could not load protocol rules from the backend ({loadError}). This is a real error, not an
            empty config — fix the API/connection and reload rather than treating the table as empty.
          </div>
        </div>
      ) : null}

      <ConfigConsole rules={rules} initialCategory={initialCategory} />

      {/* How a published rule maps to live work */}
      <section className="card">
        <div className="hd">
          <Workflow className="ic" aria-hidden="true" />
          <h3>How this rule maps to the process</h3>
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
            published rule → <b style={{ color: "var(--ink)" }}>obligations</b> (per goat / dose) →{" "}
            <b style={{ color: "var(--ink)" }}>shed-drive SOP task</b> → proof + verify →{" "}
            <b style={{ color: "var(--ink)" }}>adherence</b> gap
          </div>
          <div className="note" style={{ marginTop: 10 }}>
            After publish, every obligation, SOP task, and adherence gap is generated from <b>this config</b>. Review the
            effect in{" "}
            <Link href="/protocol-adherence" className="lk">
              Protocol Adherence
            </Link>
          </div>
        </div>
      </section>
    </div>
  );
}
