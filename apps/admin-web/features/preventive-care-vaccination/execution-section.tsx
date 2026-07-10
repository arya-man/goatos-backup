import { Syringe } from "lucide-react";
import { VaccinationExecutionBoard } from "@/features/vaccination-execution";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ApiResult, VaccinationExecutionResponse } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

// Shed-event execution — the mock's "drive — shed events" section: the physical park → shed → stage →
// drive rows with owner chain, stock, proof, and verification status. It is a NORMAL stacked section of
// the single /vaccination screen (NOT a tab and NOT a separate Parks route). Powered by the
// vaccination-execution read model (GET /vaccination/execution); shed rows deep-link to
// /vaccination/execution/sheds/{shedId}. Park scope comes from the shell top bar.
export async function VaccinationExecutionSection({
  searchParams,
  pageContract,
  executionResult,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
  executionResult?: ApiResult<VaccinationExecutionResponse>;
}) {
  return (
    <section id="execution" style={{ scrollMarginTop: 80 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 8, margin: "0 2px 10px" }}>
        <Syringe className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3 style={{ margin: 0, fontSize: 16 }}>{copy(pageContract, "section.shed_events.title")}</h3>
        <span className="sp" style={{ flex: 1 }} />
        <span className="small muted">{copy(pageContract, "section.shed_events.note")}</span>
      </div>
      <VaccinationExecutionBoard searchParams={searchParams} basePath="/vaccination" pageContract={pageContract} executionResult={executionResult} />
    </section>
  );
}
