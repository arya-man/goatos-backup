import { controlEnabled, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { requireAdminWebPageContract } from "@/lib/api/server";
import { getMarketConfig, getMarketReporters } from "@/lib/api/market-server";
import { MarketConfigSection } from "./market-config-section";
import { MarketReportersSection } from "./market-reporters-section";

/**
 * The market survey UNDER the Sales SOP library (maintainer instruction 2026-09-19: "what
 * questions, whom they are going to -- everything on that page should be configurable"). The
 * cities and questions card is the same one Sales Config mounts, so the two pages can never
 * disagree; reporters are new here. Copy and gating come from the Sales Config contract, the
 * page that owns the market survey's words.
 */
export async function MarketSurveyOnSop() {
  const [pageContract, configResult, reporters] = await Promise.all([
    requireAdminWebPageContract("sales-config"),
    getMarketConfig(),
    getMarketReporters(),
  ]);
  const canConfigure = controlEnabled(pageContract as AdminUiPageContract, "market_config_write", false);
  return (
    <div className="grid" style={{ gap: 16, marginTop: 16 }} data-testid="sales-sop-market">
      <MarketConfigSection pageContract={pageContract} configResult={configResult} canConfigure={canConfigure} />
      <MarketReportersSection pageContract={pageContract} result={reporters} canConfigure={canConfigure} />
    </div>
  );
}
