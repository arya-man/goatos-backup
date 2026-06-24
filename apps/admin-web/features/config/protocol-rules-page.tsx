import Link from "next/link";
import { AlertTriangle, Workflow } from "lucide-react";
import { ConfigConsole, type ConfigRuleRow } from "./config-console";
import { CATEGORIES } from "./rule-dsl";

// The generic CEO/COO authoring surface (obligation-engine §2.1 config-UI contract). One Config screen
// authors every protocol category; the engine, obligations, SOP tasks, and adherence all flow from
// PUBLISHED, source-backed rules. Field / verifier / park users never reach this screen — they only
// see generated obligations + SOP tasks.

function resolveCategory(category: string): string {
  return CATEGORIES.includes(category) ? category : "vaccination";
}

// Rendered at Admin / Data Ops / Config. This is the generic protocol authority surface: PHC/
// Vaccination links here with category=vaccination, but no module owns the screen.
export function ConfigProtocolRulesPage({ category }: { category: string }) {
  const initialCategory = resolveCategory(category);
  // Rules come from the backend protocol list. No list endpoint exists yet, so this is empty and the
  // table renders an honest empty state — rows are never fabricated. Wire to the list API when it lands.
  const rules: ConfigRuleRow[] = [];

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
