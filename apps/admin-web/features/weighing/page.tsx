import Link from "@/components/no-prefetch-link";
import { AlertTriangle, CalendarDays, CheckCircle2, ClipboardList, Edit3, Eye, Play, Scale, Send, Video } from "lucide-react";
import { revalidatePath } from "next/cache";
import { Tag, type Tone } from "@/components/ui-primitives";
import { createWeighingCampaign, publishWeighingCampaign } from "@/lib/api/server";
import type { AdminUiPageContract } from "@/lib/admin-ui-contract";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { fmtDate } from "@/lib/format";
import { getWeighingPageData, roleFromSearchParam, type WeighingCampaignState, type WeighingCategory, type WeighingPlanner, type WeighingScopeStatus } from "./data";

async function createWeighingCampaignAction(formData: FormData) {
  "use server";
  if (String(formData.get("duplicate_blocked") || "") === "true") {
    revalidatePath("/weighing");
    return;
  }
  const selectedShedIds = formData.getAll("shed_id").map(String);
  const categoryByShed = new Map(formData.getAll("shed_category").map((value) => {
    const [id, category] = String(value).split(":");
    return [id, category as WeighingCategory];
  }));
  const labelByShed = new Map(formData.getAll("shed_label").map((value) => {
    const [id, label] = String(value).split(":", 2);
    return [id, label];
  }));
  const sheds = selectedShedIds.map((id) => ({
    location_id: id,
    location_type: "shed" as const,
    display_name: labelByShed.get(id) || "Selected shed",
    weighing_category: categoryByShed.get(id) || "individual_animal",
  }));
  const result = await createWeighingCampaign({
    park_id: String(formData.get("park_id") || ""),
    period_start_date: String(formData.get("period_start_date") || ""),
    period_end_date: String(formData.get("period_end_date") || ""),
    start_business_date: String(formData.get("start_business_date") || ""),
    planned_cap_per_day: Number(formData.get("planned_cap_per_day") || 100),
    operator_user_id: String(formData.get("operator_user_id") || ""),
    sheds,
  });
  if (result.ok && formData.get("publish") === "true") {
    await publishWeighingCampaign(result.data.campaign.campaign_id);
  }
  revalidatePath("/weighing");
}

const statusTone: Record<WeighingCampaignState | WeighingScopeStatus | "no_task", Tone> = {
  no_task: "mut",
  draft: "mut",
  published: "info",
  in_progress: "info",
  delayed: "warn",
  completed: "ok",
  pending: "mut",
  needs_review: "warn",
};

const categoryTone: Record<WeighingCategory, Tone> = {
  individual_animal: "info",
  per_shed_partition: "pur",
};

const statusLabel: Record<WeighingCampaignState | WeighingScopeStatus | "no_task", string> = {
  no_task: "No task",
  draft: "Draft",
  published: "Published",
  in_progress: "In progress",
  delayed: "Delayed",
  completed: "Completed",
  pending: "Pending",
  needs_review: "Needs review",
};

const categoryLabel: Record<WeighingCategory, string> = {
  individual_animal: "Individual",
  per_shed_partition: "Shed total",
};

function reviewLabel(value: string): string {
  switch (value) {
    case "icu":
      return "In ICU";
    case "sold_transferred":
      return "Sold / transferred";
    case "dead":
      return "Dead";
    default:
      return "Unavailable";
  }
}

function pct(done: number, total: number): number {
  if (total <= 0) return 0;
  return Math.max(0, Math.min(100, Math.round((done / total) * 100)));
}

function CapabilityButton({
  enabled,
  children,
  icon,
}: {
  enabled: boolean;
  children: React.ReactNode;
  icon: React.ReactNode;
}) {
  return (
    <button className={enabled ? "btn sm" : "btn sm ghost"} type="button" aria-disabled={!enabled}>
      {icon}
      {children}
    </button>
  );
}

export async function WeighingPage({ searchParams, pageContract }: { searchParams?: RouteSearchParams; pageContract: AdminUiPageContract }) {
  const role = roleFromSearchParam(one(searchParams ?? {}, "role"));
  const result = await getWeighingPageData(role);
  if (!result.ok) {
    return (
      <div className="screen on weighing-page">
        <section className="card pad">
          <h1>Weighing unavailable</h1>
          <p className="muted">{result.error.message}</p>
        </section>
      </div>
    );
  }

  const { campaign, weeks, planner } = result.data;
  const individualPct = pct(campaign.individualCompleted, campaign.individualExpected);
  const shedPct = pct(campaign.shedPartitionCompleted, campaign.shedPartitionExpected);

  return (
    <div className="screen on weighing-page">
      <div className="phead">
        <div>
          <div className="crumb">Preventive Care (PC) / Weighing</div>
          <h1>{pageContract.title}</h1>
          <div className="sub">{pageContract.subtitle}</div>
        </div>
        <div className="sp" />
      </div>

      <section className="card weighing-hero">
        <div className="hd">
          <Scale className="ic" aria-hidden="true" />
          <h3>{campaign.weekLabel} campaign</h3>
          <div className="sp" />
          <Tag tone={statusTone[campaign.state]}>{statusLabel[campaign.state]}</Tag>
        </div>
        <div className="bd weighing-hero-grid">
          <div>
            <div className="weighing-week-row" aria-label="Week selector">
              {weeks.map((week) => (
                <Link key={week.key} href={`/weighing?role=${role}&week=${week.key}`} className={`weighing-week${week.key === campaign.weekStart ? " on" : ""}`} scroll={false}>
                  <span>{week.label}</span>
                  <Tag tone={statusTone[week.state]}>{statusLabel[week.state]}</Tag>
                </Link>
              ))}
            </div>
            <div className="metagrid weighing-meta">
              <div>
                <div className="k">Lane</div>
                <div className="v">{campaign.laneLabel}</div>
              </div>
              <div>
                <div className="k">Start business date</div>
                <div className="v">{fmtDate(campaign.startBusinessDate)}</div>
              </div>
              <div>
                <div className="k">Selected scopes</div>
                <div className="v">{campaign.selectedScopes} shed/partitions</div>
              </div>
              <div>
                <div className="k">Operator</div>
                <div className="v">{campaign.operatorName}</div>
              </div>
            </div>
          </div>
          <div className="weighing-command-panel">
            <div className="weighing-command-title">
              {campaign.reviewOnly ? <Eye className="ic" aria-hidden="true" /> : campaign.canExecute ? <Play className="ic" aria-hidden="true" /> : <Edit3 className="ic" aria-hidden="true" />}
              {campaign.reviewOnly ? "Monitor / review only" : campaign.canExecute ? "Operator execution view" : "Leadership planner"}
            </div>
            <p className="muted small">
              {campaign.reviewOnly
                ? "Director persona can review progress and evidence but cannot create, publish, edit, or execute."
                : campaign.canExecute
                  ? "Operator persona sees execution state but planning actions remain disabled."
                  : "CEO/CXO can create, edit, and publish weekly kids weighing tasks."}
            </p>
            <div className="weighing-actions">
              <CapabilityButton enabled={campaign.canCreate} icon={<Edit3 className="ic" aria-hidden="true" />}>Create/edit</CapabilityButton>
              <CapabilityButton enabled={campaign.canPublish} icon={<Send className="ic" aria-hidden="true" />}>Publish</CapabilityButton>
              <CapabilityButton enabled={campaign.canExecute} icon={<Play className="ic" aria-hidden="true" />}>Execute</CapabilityButton>
            </div>
          </div>
        </div>
      </section>

      {campaign.canCreate ? <WeighingPlannerCard planner={planner} /> : null}

      <section className="weighing-metrics">
        <div className="card pad weighing-metric">
          <div className="k">Individual animal</div>
          <div className="v">{campaign.individualCompleted}<span>/{campaign.individualExpected}</span></div>
          <div className="weighing-bar"><i style={{ width: `${individualPct}%` }} /></div>
          <p className="muted small">RFID + animal identity + weight + mandatory per-animal video.</p>
        </div>
        <div className="card pad weighing-metric">
          <div className="k">Per shed/partition</div>
          <div className="v">{campaign.shedPartitionCompleted}<span>/{campaign.shedPartitionExpected}</span></div>
          <div className="weighing-bar pur"><i style={{ width: `${shedPct}%` }} /></div>
          <p className="muted small">Selected-scope result + scope proof; no individual weight update.</p>
        </div>
        <div className="card pad weighing-metric">
          <div className="k">Needs attention</div>
          <div className="v">{campaign.wrongShedScans + campaign.unavailableAnimals + campaign.proofPending}</div>
          <div className="weighing-attention">
            <Tag tone="warn">{campaign.wrongShedScans} wrong shed</Tag>
            <Tag tone="pur">{campaign.unavailableAnimals} unavailable</Tag>
            <Tag tone="info">{campaign.proofPending} proof pending</Tag>
          </div>
        </div>
      </section>

      <section className="card">
        <div className="hd">
          <ClipboardList className="ic" aria-hidden="true" />
          <h3>Shed / partition work groups</h3>
          <div className="sp" />
          <span className="muted small">Category-aware progress; grouping is preserved.</span>
        </div>
        <div className="bd" style={{ padding: 0, overflowX: "auto" }}>
          <table className="weighing-table">
            <thead>
              <tr>
                <th>Park</th>
                <th>Shed / partition</th>
                <th>Category</th>
                <th>Progress</th>
                <th>Proof</th>
                <th>Wrong shed</th>
                <th>Planned</th>
                <th>Current date</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {campaign.scopes.map((row) => (
                <tr key={row.id}>
                  <td>{row.parkName}</td>
                  <td>
                    <b>{row.shedName}</b>
                    <span className="muted small blockish">{row.partitionName}</span>
                  </td>
                  <td><Tag tone={categoryTone[row.category]}>{categoryLabel[row.category]}</Tag></td>
                  <td>
                    <b>{row.completedCount}</b> / {row.expectedCount}
                    <span className="muted small blockish">{row.remainingCount} remaining · {row.unavailableCount} unavailable</span>
                  </td>
                  <td><Tag tone={row.proofPendingCount > 0 ? "warn" : "ok"}><Video className="ic" aria-hidden="true" />{row.proofPendingCount > 0 ? `${row.proofPendingCount} pending` : "linked"}</Tag></td>
                  <td>{row.wrongShedCount > 0 ? <Tag tone="warn">{row.wrongShedCount} visible</Tag> : <span className="muted">-</span>}</td>
                  <td>{fmtDate(row.plannedDate)}</td>
                  <td>{fmtDate(row.effectiveDate)}</td>
                  <td><Tag tone={statusTone[row.status]}>{statusLabel[row.status]}</Tag></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <div className="weighing-review-grid">
        <section className="card">
          <div className="hd">
            <AlertTriangle className="ic" aria-hidden="true" />
            <h3>Wrong-shed scans</h3>
            <div className="sp" />
            <Tag tone="warn">expected vs actual</Tag>
          </div>
          <div className="bd" style={{ padding: 0, overflowX: "auto" }}>
            <table className="weighing-table compact">
              <thead>
                <tr>
                  <th>Animal</th>
                  <th>Expected / original</th>
                  <th>Actual / current</th>
                  <th>Scan</th>
                </tr>
              </thead>
              <tbody>
                {campaign.wrongShedRows.map((row) => (
                  <tr key={row.id}>
                    <td><b>{row.animalDisplayId}</b><span className="muted small blockish">{row.rfid}</span></td>
                    <td>{row.expectedShed}<span className="muted small blockish">{row.originalPartition}</span></td>
                    <td>{row.actualShed}<span className="muted small blockish">{row.currentPartition}</span></td>
                    <td>{fmtDate(row.scannedAt)}<span className="muted small blockish">{row.operatorName}</span></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>

        <section className="card">
          <div className="hd">
            <CheckCircle2 className="ic" aria-hidden="true" />
            <h3>Missing / unavailable</h3>
            <div className="sp" />
            <Tag tone="pur">current herd truth</Tag>
          </div>
          <div className="bd" style={{ padding: 0, overflowX: "auto" }}>
            <table className="weighing-table compact">
              <thead>
                <tr>
                  <th>Animal</th>
                  <th>Expected</th>
                  <th>Classification</th>
                  <th>Checked</th>
                </tr>
              </thead>
              <tbody>
                {campaign.missingRows.map((row) => (
                  <tr key={row.id}>
                    <td><b>{row.animalDisplayId}</b></td>
                    <td>{row.expectedShed}</td>
                    <td><Tag tone="pur">{reviewLabel(row.classification)}</Tag><span className="muted small blockish">{row.currentTruth}</span></td>
                    <td>{fmtDate(row.checkedAt)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      </div>

      <section className="card weighing-integration">
        <div className="hd">
          <CalendarDays className="ic" aria-hidden="true" />
          <h3>Integration notes</h3>
        </div>
        <div className="bd">
          <p className="muted small">
            This admin-web slice reads Weighing campaigns through the generated OpenAPI client. Command buttons
            reflect backend roles and remain disabled for director/operator personas.
          </p>
        </div>
      </section>
    </div>
  );
}

function WeighingPlannerCard({ planner }: { planner: WeighingPlanner }) {
  const duplicateBlocked = planner.duplicateBlocked && planner.existingCampaignId;
  const title = duplicateBlocked ? "Edit weekly kids weighing task" : "Create weekly kids weighing task";
  const taskLabel = duplicateBlocked ? "Scheduled task" : "New task";
  return (
    <section className="card weighing-planner">
      <div className="hd">
        <Edit3 className="ic" aria-hidden="true" />
        <h3>{title}</h3>
        <div className="sp" />
        {duplicateBlocked ? <Tag tone="warn">Already scheduled</Tag> : <Tag tone="ok">CEO / CXO</Tag>}
      </div>
      <div className="bd">
        <form action={createWeighingCampaignAction} className="weighing-plan-form">
          <input type="hidden" name="duplicate_blocked" value={duplicateBlocked ? "true" : "false"} />
          <input type="hidden" name="park_id" value={planner.selectedParkId} />
          <input type="hidden" name="period_start_date" value={planner.periodStartDate} />
          <input type="hidden" name="period_end_date" value={planner.periodEndDate} />
          <input type="hidden" name="start_business_date" value={planner.startBusinessDate} />
          <input type="hidden" name="planned_cap_per_day" value={planner.plannedCapPerDay} />
          <input type="hidden" name="operator_user_id" value={planner.selectedOperatorId} />
          {planner.sheds.map((shed) => (
            <span key={shed.id}>
              {shed.selected ? <input type="hidden" name="shed_id" value={shed.id} /> : null}
              <input type="hidden" name="shed_category" value={`${shed.id}:${shed.category}`} />
              <input type="hidden" name="shed_label" value={`${shed.id}:${shed.label}`} />
            </span>
          ))}
          <div className="weighing-wizard-head">
            <div>
              <div className="crumb">{planner.weekLabel}</div>
              <h2>{taskLabel}</h2>
              <p className="muted">
                {duplicateBlocked
                  ? "This park already has a weekly kids weighing task for the selected week. Edit that task instead of creating a duplicate."
                  : "Leadership chooses the park, shed partitions, category per selected scope, and the operator before publishing."}
              </p>
            </div>
            <div className="weighing-step-bars" aria-label="Planner progress">
              <i /><i /><i /><i /><i />
            </div>
          </div>

          <div className="weighing-builder-grid">
            <div className="weighing-builder-step">
              <div className="weighing-step-label">Step 1 · Lane</div>
              <h3>Confirm the lane</h3>
              <div className="weighing-choice on">
                <span className="weighing-radio" />
                <div><b>Weekly · Kids</b><small>category is set per shed in step 3</small></div>
              </div>
              <div className="weighing-choice disabled">
                <span className="weighing-radio" />
                <div><b>Monthly · Adults</b><small>excluded in v1</small></div>
              </div>
            </div>

            <div className="weighing-builder-step">
              <div className="weighing-step-label">Step 2 · Park</div>
              <h3>Select one park</h3>
              {planner.parks.map((park) => (
                <div className={`weighing-choice${park.selected ? " on" : ""}`} key={park.id}>
                  <span className="weighing-radio" />
                  <div><b>{park.label}</b><small>{park.subtitle}</small></div>
                  <strong>{park.kidCount}</strong>
                </div>
              ))}
            </div>

            <div className="weighing-builder-step weighing-shed-step">
              <div className="weighing-step-label">Step 3 · Sheds</div>
              <h3>Sheds & category</h3>
              <p className="muted small">Category decides the operator capture screen and required proof.</p>
              {planner.sheds.map((shed) => (
                <div className={`weighing-shed-choice${shed.selected ? " on" : ""}`} key={shed.id}>
                  <span className="weighing-check">{shed.selected ? "✓" : ""}</span>
                  <div className="weighing-shed-main">
                    <b>{shed.label}</b>
                    <small>{shed.subtitle}</small>
                    {shed.selected ? (
                      <div className="weighing-segment" aria-label={`${shed.label} category`}>
                        <span className={shed.category === "individual_animal" ? "on individual" : ""}>Individual</span>
                        <span className={shed.category === "per_shed_partition" ? "on lumpsum" : ""}>Lumpsum</span>
                      </div>
                    ) : null}
                  </div>
                  <strong>{shed.kidCount}</strong>
                </div>
              ))}
              <div className="weighing-total-card">
                <span>{planner.individualShedCount} individual · {planner.lumpsumShedCount} lumpsum</span>
                <b>{planner.individualKidCount}</b>
                <small>weighed individually</small>
              </div>
            </div>

            <div className="weighing-builder-step">
              <div className="weighing-step-label">Step 5 · Assign</div>
              <h3>Assign operator</h3>
              <p className="muted small">Publishing creates open work. Director personas stay monitor-only.</p>
              {planner.operators.map((operator) => (
                <div className={`weighing-choice${operator.selected ? " on" : ""}${operator.disabled ? " disabled" : ""}`} key={operator.id}>
                  <span className="weighing-radio" />
                  <div><b>{operator.name}</b><small>{operator.capabilityLabel}</small></div>
                </div>
              ))}
              <div className="weighing-assign-summary">
                <span>Individual</span><b>{planner.individualShedCount} sheds · {planner.individualKidCount} kids</b>
                <span>Lumpsum</span><b>{planner.lumpsumShedCount} sheds · {planner.lumpsumKidCount} in scope</b>
              </div>
            </div>
          </div>

          <div className="weighing-planner-actions">
            {duplicateBlocked ? (
              <>
                <Link className="btn ghost" href={`/weighing?campaign=${planner.existingCampaignId}`}>Edit task</Link>
                <button className="btn" type="button" disabled>New task blocked</button>
              </>
            ) : (
              <>
                <button className="btn ghost" type="submit" name="publish" value="false">Save draft</button>
                <button className="btn primary" type="submit" name="publish" value="true"><Send className="ic" aria-hidden="true" /> Publish</button>
              </>
            )}
          </div>
        </form>
      </div>
    </section>
  );
}
