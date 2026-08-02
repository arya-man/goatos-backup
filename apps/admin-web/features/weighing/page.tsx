import Link from "@/components/no-prefetch-link";
import { CalendarDays, ClipboardList, Edit3, Eye, Play, Scale, Send, Video } from "lucide-react";
import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { Tag, type Tone } from "@/components/ui-primitives";
import { createWeighingCampaign, publishWeighingCampaign, updateWeighingCampaign } from "@/lib/api/server";
import type { AdminUiPageContract } from "@/lib/admin-ui-contract";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { fmtDate } from "@/lib/format";
import { getWeighingPageData, roleFromSearchParam, type WeighingCampaignState, type WeighingCategory, type WeighingPlanner, type WeighingScopeStatus } from "./data";
import { ParkSelector } from "./park-selector";

async function createWeighingCampaignAction(formData: FormData) {
  "use server";
  const campaignId = String(formData.get("campaign_id") || "");
  if (String(formData.get("duplicate_blocked") || "") === "true") {
    revalidatePath("/weighing");
    redirect("/weighing?notice=duplicate-blocked");
  }
  const selectedParkId = String(formData.get("park_id") || "");
  const selectedShedIds = formData.getAll(`shed_id_${selectedParkId}`).map(String);
  const labelByShed = new Map(formData.getAll("shed_label").map((value) => {
    const [id, label] = String(value).split(":", 2);
    return [id, label];
  }));
  const parkByShed = new Map(formData.getAll("shed_park_id").map((value) => {
    const [id, parkId] = String(value).split(":", 2);
    return [id, parkId];
  }));
  const validSelectedShedIds = selectedShedIds.filter((id) => parkByShed.get(id) === selectedParkId);
  if (validSelectedShedIds.length !== selectedShedIds.length || validSelectedShedIds.length === 0) {
    revalidatePath("/weighing");
    redirect("/weighing?notice=create-failed");
  }
  const sheds = validSelectedShedIds.map((id) => ({
    location_id: id,
    location_type: "shed" as const,
    display_name: labelByShed.get(id) || "Selected shed",
    weighing_category: (String(formData.get(`shed_category_${id}`) || "individual_animal") as WeighingCategory),
  }));
  const body = {
    park_id: selectedParkId,
    period_start_date: String(formData.get("period_start_date") || ""),
    period_end_date: String(formData.get("period_end_date") || ""),
    start_business_date: String(formData.get("start_business_date") || ""),
    planned_cap_per_day: Number(formData.get("planned_cap_per_day") || 100),
    operator_user_id: String(formData.get("operator_user_id") || ""),
    sheds,
  };
  const result = campaignId
    ? await updateWeighingCampaign(campaignId, body)
    : await createWeighingCampaign(body);
  if (!result.ok) {
    revalidatePath("/weighing");
    redirect("/weighing?notice=create-failed");
  }
  if (formData.get("publish") === "true") {
    const publishResult = await publishWeighingCampaign(result.data.campaign.campaign_id);
    revalidatePath("/weighing");
    redirect(`/weighing?notice=${publishResult.ok ? "published" : "publish-failed"}`);
  }
  revalidatePath("/weighing");
  redirect("/weighing?notice=draft-saved");
}

const statusTone: Record<WeighingCampaignState | WeighingScopeStatus | "no_task", Tone> = {
  no_task: "mut",
  draft: "mut",
  published: "info",
  in_progress: "info",
  delayed: "warn",
  completed: "ok",
  closed: "ok",
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
  closed: "Closed",
  pending: "Pending",
  needs_review: "Needs review",
};

const categoryLabel: Record<WeighingCategory, string> = {
  individual_animal: "Individual",
  per_shed_partition: "Shed total",
};

function noticeFromSearch(value: string | undefined): { tone: "ok" | "warn" | "err"; title: string; body: string } | null {
  switch (value) {
    case "published":
      return {
        tone: "ok",
        title: "Task published",
        body: "Amit now sees the selected shed work. The campaign stays published until the first capture starts it.",
      };
    case "draft-saved":
      return {
        tone: "ok",
        title: "Draft saved",
        body: "The weekly kids plan is saved without opening operator work yet.",
      };
    case "duplicate-blocked":
      return {
        tone: "warn",
        title: "Create blocked",
        body: "This park already has a task for the selected week. Use Edit existing task so captures and history stay attached.",
      };
    case "create-failed":
      return {
        tone: "err",
        title: "Task was not created",
        body: "The backend rejected the create request. No duplicate task was created.",
      };
    case "publish-failed":
      return {
        tone: "err",
        title: "Draft saved, publish failed",
        body: "The task exists as a draft. Publish again after checking backend validation.",
      };
    default:
      return null;
  }
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
  const result = await getWeighingPageData(
    role,
    one(searchParams ?? {}, "week"),
    one(searchParams ?? {}, "campaign"),
    one(searchParams ?? {}, "park"),
  );
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
  const notice = noticeFromSearch(one(searchParams ?? {}, "notice"));
  // NO EXPECTED-ANIMAL DENOMINATOR (maintainer decision 2026-07-31). Weighing is
  // free-flow: the operator scans whatever is in front of them, so there is no known
  // total. A "completed / expected" bar asserted a completeness the data never had.
  // Captured counts only.

  return (
    <div className="screen on weighing-page">
      {notice ? <WeighingNotice tone={notice.tone} title={notice.title} body={notice.body} /> : null}
      <div className="phead">
        <div>
          <div className="crumb">Weighing</div>
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
          <div className="v">{campaign.individualCompleted}<span> captured</span></div>
          <p className="muted small">Free-flow RFID/tag bucket + weight + mandatory per-row video.</p>
        </div>
        <div className="card pad weighing-metric">
          <div className="k">Per shed/partition</div>
          <div className="v">{campaign.shedPartitionCompleted}<span> captured</span></div>
          <p className="muted small">Free-flow shed bucket result + total count + at least one synced video.</p>
        </div>
        <div className="card pad weighing-metric">
          <div className="k">Needs attention</div>
          <div className="v">{campaign.proofPending}</div>
          <div className="weighing-attention">
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
                  <td
                    aria-disabled={!row.capturedCountIsBacked}
                    style={{ opacity: row.capturedCountIsBacked ? 1 : 0.6 }}
                    title={!row.capturedCountIsBacked ? "Weighing is free-flow; backend field 'per_shed_captured_count' required for honest count" : undefined}
                  >
                    {row.capturedCountIsBacked ? <><b>{row.completedCount}</b> captured</> : "captured (n/a)"}
                  </td>
                  <td>
                    {row.readyToClose ? (
                      <Tag tone="ok"><Video className="ic" aria-hidden="true" />linked</Tag>
                    ) : row.proofPendingCount > 0 ? (
                      <Tag tone="warn"><Video className="ic" aria-hidden="true" />{row.proofPendingCount} pending</Tag>
                    ) : (
                      <Tag tone="mut"><Video className="ic" aria-hidden="true" />not submitted</Tag>
                    )}
                  </td>
                  <td>{fmtDate(row.plannedDate)}</td>
                  <td>{fmtDate(row.effectiveDate)}</td>
                  <td><Tag tone={statusTone[row.status]}>{statusLabel[row.status]}</Tag></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

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

function WeighingNotice({ tone, title, body }: { tone: "ok" | "warn" | "err"; title: string; body: string }) {
  return (
    <div className={`weighing-toast ${tone}`} role={tone === "err" ? "alert" : "status"} aria-live="polite">
      <b>{title}</b>
      <span>{body}</span>
    </div>
  );
}

function WeighingPlannerCard({ planner }: { planner: WeighingPlanner }) {
  const duplicateBlocked = planner.duplicateBlocked && planner.existingCampaignId;
  const title = duplicateBlocked ? "Edit weekly kids weighing task" : "Create weekly kids weighing task";
  const taskLabel = duplicateBlocked ? "Scheduled task" : "New task";
  const selectedPark = planner.parks.find((park) => park.id === planner.selectedParkId);
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
          {planner.editingCampaignId ? <input type="hidden" name="campaign_id" value={planner.editingCampaignId} /> : null}
          <input type="hidden" name="period_start_date" value={planner.periodStartDate} />
          <input type="hidden" name="period_end_date" value={planner.periodEndDate} />
          <input type="hidden" name="start_business_date" value={planner.startBusinessDate} />
          <input type="hidden" name="planned_cap_per_day" value={planner.plannedCapPerDay} />
          {planner.sheds.map((shed) => (
            <span key={shed.id}>
              <input type="hidden" name="shed_label" value={`${shed.id}:${shed.label}`} />
              <input type="hidden" name="shed_park_id" value={`${shed.id}:${shed.parkId}`} />
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

          {duplicateBlocked ? (
            <div className="weighing-duplicate-grid">
              <div className="weighing-duplicate-card">
                <Tag tone="warn">task already exists</Tag>
                <h3>{selectedPark?.label.split(" · ")[0] || "This park"} · {planner.weekLabel.split(" · ")[0]}</h3>
                <p>This park already has a weighing task for this week. One task per park per week; create is blocked.</p>
              </div>
              <div className="weighing-existing-task">
                <span>Existing task</span><b>{planner.existingCampaignWeekLabel || planner.weekLabel}</b>
                <span>Status</span><b><Tag tone={statusTone[planner.existingCampaignState || "draft"]}>{statusLabel[planner.existingCampaignState || "draft"]}</Tag></b>
                <span>Operator</span><b>{planner.existingCampaignOperatorName || "Operator not reported by API"}</b>
                <span>Sheds</span><b>{planner.existingCampaignShedCount ?? planner.sheds.filter((shed) => shed.selected).length}</b>
              </div>
              <div className="weighing-duplicate-note">
                Editing keeps the same campaign, its history, and every accepted capture. It never creates a second task for {selectedPark?.label.split(" · ")[0] || "this park"}.
              </div>
            </div>
          ) : null}

          <div className={`weighing-builder-grid${duplicateBlocked ? " muted" : ""}`}>
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

            <ParkSelector planner={planner} />

            <div className="weighing-builder-step weighing-shed-step">
              <div className="weighing-step-label">Step 3 · Sheds</div>
              <h3>Sheds & category</h3>
              <p className="muted small">Category decides the operator capture screen and required proof.</p>
              <div className="weighing-proof-note">
                <b>Proof rule</b>
                <span>Individual needs RFID + weight + per-animal video. Lumpsum records one shed total with one scope video and never updates kid latest trusted weights.</span>
              </div>
              {planner.sheds.map((shed) => (
                <div className={`weighing-shed-choice${shed.selected ? " on" : ""}`} key={shed.id}>
                  <input type="checkbox" name={`shed_id_${shed.parkId}`} value={shed.id} defaultChecked={shed.selected} />
                  <span className="weighing-check">{shed.selected ? "✓" : ""}</span>
                  <div className="weighing-shed-main">
                    <b>{shed.label}</b>
                    <small>{shed.subtitle}</small>
                    <div className="weighing-segment" aria-label={`${shed.label} category`}>
                      <label className={shed.category === "individual_animal" ? "on individual" : ""}>
                        <input type="radio" name={`shed_category_${shed.id}`} value="individual_animal" defaultChecked={shed.category === "individual_animal"} />
                        Individual
                      </label>
                      <label className={shed.category === "per_shed_partition" ? "on lumpsum" : ""}>
                        <input type="radio" name={`shed_category_${shed.id}`} value="per_shed_partition" defaultChecked={shed.category === "per_shed_partition"} />
                        Lumpsum
                      </label>
                    </div>
                  </div>
                  <strong>{shed.kidCount}</strong>
                </div>
              ))}
              {planner.shedListTruncated && (
                <div className="weighing-truncation-notice" style={{ padding: "8px 12px", marginTop: "8px", borderRadius: "4px", backgroundColor: "var(--bg-attention)", color: "var(--fg-warn)", fontSize: "12px" }}>
                  <strong>Shed list is truncated at the 500-shed display cap:</strong> This park has more sheds than can be shown. Save and Publish are disabled below -- a partial shed list must never be saved or published, as it would silently drop sheds beyond the cap. Contact admin if you need sheds beyond this list.
                </div>
              )}
              <div className="weighing-total-card">
                <span>{planner.individualShedCount} individual · {planner.lumpsumShedCount} lumpsum</span>
                <b>{planner.individualKidCount}</b>
                <small>weighed individually</small>
              </div>
            </div>

            <div className="weighing-builder-step">
              <div className="weighing-step-label">Step 4 · Plan / Step 5 · Assign</div>
              <h3>Assign operator</h3>
              <p className="muted small">Publishing creates open work. Director personas stay monitor-only.</p>
              <div className="weighing-plan-preview">
                <span>Day 1</span><b>Lumpsum scopes first</b>
                <span>Day 2</span><b>Individual RFID rows</b>
                <span>Day 3</span><b>Rollover and remaining sheds</b>
              </div>
              {planner.operators.map((operator) => (
                <label className={`weighing-choice${operator.selected ? " on" : ""}${operator.disabled ? " disabled" : ""}`} key={operator.id}>
                  <input type="radio" name="operator_user_id" value={operator.id} defaultChecked={operator.selected} disabled={operator.disabled} />
                  <span className="weighing-radio" />
                  <div><b>{operator.name}</b><small>{operator.capabilityLabel}</small></div>
                </label>
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
                <Link className="btn ghost" href="/weighing">Cancel</Link>
                <Link className="btn primary" href={`/weighing?campaign=${planner.existingCampaignId}`}>Edit existing task</Link>
              </>
            ) : (
              <>
                <div className="weighing-action-note" aria-live="polite">
                  {planner.shedListTruncated
                    ? "Save and Publish are disabled: the shed list is truncated at the 500-shed display cap, and saving now would silently drop sheds beyond it."
                    : "Publish opens the selected operator's work queue. Save draft keeps it invisible to operators."}
                </div>
                <button
                  className="btn ghost"
                  type="submit"
                  name="publish"
                  value="false"
                  disabled={planner.shedListTruncated}
                  aria-disabled={planner.shedListTruncated}
                  title={planner.shedListTruncated ? "Disabled: shed list truncated at the 500-shed display cap" : undefined}
                >
                  Save draft
                </button>
                <button
                  className="btn primary"
                  type="submit"
                  name="publish"
                  value="true"
                  disabled={planner.shedListTruncated}
                  aria-disabled={planner.shedListTruncated}
                  title={planner.shedListTruncated ? "Disabled: shed list truncated at the 500-shed display cap" : undefined}
                >
                  <Send className="ic" aria-hidden="true" /> Publish
                </button>
              </>
            )}
          </div>
        </form>
      </div>
    </section>
  );
}
