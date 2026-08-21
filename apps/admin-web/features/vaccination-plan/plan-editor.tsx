"use client";

/**
 * Vaccination plan editor — screen 2 of the console.
 *
 * Markup and class names follow the approved mock; styles live in
 * app/mesha-theme.css under `.vp`.
 *
 * Everything on this screen edits a DRAFT. Nothing here reaches the field until
 * Publish, which is why the action bar is pinned to the bottom rather than
 * sitting above a long scroll: the decision to publish is made after reading,
 * not before.
 */

import { useRouter } from "next/navigation";
import { ArrowLeft, Check } from "lucide-react";
import { useMemo, useState, useTransition } from "react";

import { DurationField, formatDays } from "./duration-field";
import type { EditorPlan, EditorVaccine } from "./editor-model";
import { publishPlan, saveDraftPlan } from "./plan-actions";
import { humanDays } from "./plan-model";

type Props = {
  protocolId: string;
  draftVersionId: string;
  draftLabel: string;
  liveLabel: string | null;
  liveSince: string | null;
  scopeLabel: string;
  originalRuleDsl: unknown;
  proofPolicy: unknown;
  initialPlan: EditorPlan;
  impact: ImpactSummary | null;
  canPublish: boolean;
  cannotPublishReason: string | null;
};

export type ImpactSummary = {
  eligibleAnimals: number;
  affectedSheds: number;
  estimatedDays: number;
  dailyCap: number;
  capacityStatus?: string;
};

export function VaccinationPlanEditor(props: Props) {
  const router = useRouter();
  const [plan, setPlan] = useState<EditorPlan>(props.initialPlan);
  const [selected, setSelected] = useState(props.initialPlan.vaccines[0]?.code ?? "");
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const dirty = useMemo(
    () => JSON.stringify(plan) !== JSON.stringify(props.initialPlan),
    [plan, props.initialPlan],
  );
  const current = plan.vaccines.find((v) => v.code === selected) ?? plan.vaccines[0];
  const onCount = plan.vaccines.filter((v) => v.on).length;

  // A vaccine switched on with no doses is not in the plan: "off" IS an empty
  // schedule, so saving one round-trips it straight back to off and the switch
  // silently flips back. Rather than let that happen quietly, saving is blocked
  // until the vaccine has a dose or is switched off again.
  const emptyOn = plan.vaccines.filter((v) => v.on && v.kidDoses.length === 0 && v.driveDoses.length === 0);
  const blockedReason =
    emptyOn.length > 0
      ? `${emptyOn.map((v) => v.name).join(", ")} ${emptyOn.length === 1 ? "is" : "are"} switched on but ${
          emptyOn.length === 1 ? "has" : "have"
        } no doses. Add a dose, or switch ${emptyOn.length === 1 ? "it" : "them"} off.`
      : null;

  function updateVaccine(code: string, change: (v: EditorVaccine) => EditorVaccine) {
    setSaved(false);
    setPlan((p) => ({ ...p, vaccines: p.vaccines.map((v) => (v.code === code ? change(v) : v)) }));
  }

  function onSave() {
    setError(null);
    startTransition(async () => {
      const result = await saveDraftPlan(props.draftVersionId, plan, props.originalRuleDsl);
      if (!result.ok) {
        setError(result.error);
        return;
      }
      setSaved(true);
      // A save REPLACES the draft: the new version carries the edits and the old
      // row is discarded, so the id in the URL is now dead. Point the URL at the
      // new draft before refreshing -- refreshing alone re-runs the page against
      // the discarded id and 404s the editor out from under the user.
      if (result.versionId && result.versionId !== props.draftVersionId) {
        router.replace(`/vaccination/plan/edit?version=${result.versionId}`);
      }
      router.refresh();
    });
  }

  function onPublish() {
    setError(null);
    startTransition(async () => {
      // Save first: publishing what is on screen, not what was last saved.
      const savedResult = await saveDraftPlan(props.draftVersionId, plan, props.originalRuleDsl);
      if (!savedResult.ok) {
        setError(savedResult.error);
        return;
      }
      // The save REPLACED the draft, so the id in the URL is already dead. Point at
      // the new one BEFORE attempting the publish: if the publish then fails, the
      // user is still on a live draft with their edits, rather than stranded on a
      // discarded id where the next click 404s and the saved work is only reachable
      // from the list.
      const liveId = savedResult.versionId ?? props.draftVersionId;
      if (liveId !== props.draftVersionId) {
        router.replace(`/vaccination/plan/edit?version=${liveId}`);
      }
      const published = await publishPlan(liveId);
      if (!published.ok) {
        setError(published.error);
        router.refresh();
        return;
      }
      router.push("/vaccination/plan");
      router.refresh();
    });
  }

  if (!current) {
    return (
      <div className="vplan">
        <section className="card">
          <div className="card-b">
            <p>This draft has no vaccines in it.</p>
          </div>
        </section>
      </div>
    );
  }

  return (
    <div className="vplan">
      <a className="backlink" href="/vaccination/plan">
        <ArrowLeft size={14} aria-hidden /> Vaccination plan
      </a>

      <header className="head">
        <div className="head-top">
          <div>
            <div className="eyebrow">
              Editing {props.draftLabel}
              {props.liveLabel ? ` · based on ${props.liveLabel}` : ""}
            </div>
            <h1>Company vaccination plan</h1>
            <p className="sub">
              One plan decides which animal gets which vaccine, and when. Publishing it schedules
              every future vaccination task across both parks.
            </p>
          </div>
          <span className="pill">
            <span className="dot" />
            Draft · not live yet
          </span>
        </div>

        <div className="scope">
          <div>
            <div className="k">Applies to</div>
            <div className="vv">{props.scopeLabel}</div>
          </div>
          <div>
            <div className="k">Vaccines switched on</div>
            <div className="vv num">
              {onCount} of {plan.vaccines.length}
            </div>
          </div>
          <div>
            <div className="k">Replaces</div>
            <div className="vv verline">
              {props.liveLabel ? (
                <span>
                  {props.liveLabel}
                  {props.liveSince ? ` · live since ${props.liveSince}` : ""}
                </span>
              ) : (
                <span>Nothing — this is the first plan</span>
              )}
            </div>
          </div>
        </div>
        {error ? (
          <div className="alert" style={{ marginTop: 16, marginBottom: 0 }}>
            <span className="ic">!</span>
            <span>{error}</span>
          </div>
        ) : null}
      </header>

      <div className="panes">
        <div className="leftcol">
          <nav className="rail" aria-label="Vaccines">
            <div className="rail-h">
              <div className="t">Vaccines</div>
              <div className="s">
                {onCount} on · {plan.vaccines.length - onCount} off
              </div>
            </div>
            <ul className="vlist">
              {plan.vaccines.map((v) => (
                <li key={v.code}>
                  <button
                    className={`v-item${v.on ? "" : " off"}`}
                    aria-current={v.code === current.code}
                    onClick={() => setSelected(v.code)}
                    type="button"
                  >
                    <span className="sw">{initials(v.name)}</span>
                    <span className="v-txt">
                      <span className="v-name">{v.name}</span>
                      <span className="v-sched">{summarise(v)}</span>
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          </nav>
        </div>

        <div>
          <section className="card">
            <div className="card-h">
              <div>
                <h2>
                  {current.name}
                  <span className="prio">{current.vaccineClass || "—"}</span>
                </h2>
                {current.disease ? <p className="s">{current.disease}</p> : null}
              </div>
              <span className="vp-switchrow">
                <span className="vp-switchlabel">{current.on ? "In this plan" : "Switched off"}</span>
                <button
                  className="sws"
                  role="switch"
                  aria-checked={current.on}
                  aria-label={`Include ${current.name}`}
                  type="button"
                  onClick={() => updateVaccine(current.code, (v) => ({ ...v, on: !v.on }))}
                />
              </span>
            </div>

            <div className={current.on ? "card-b" : "card-b vp-off"}>
              {current.on && current.kidDoses.length === 0 && current.driveDoses.length === 0 ? (
                <p className="hintline" style={{ margin: "0 0 14px" }}>
                  This vaccine is in the plan but has no doses yet. Add at least one below, or
                  switch it off.
                </p>
              ) : null}

              {current.kidDoses.length > 0 ? (
                <>
                  <div className="sec-label">
                    {current.kidDoses.length > 1
                      ? "The doses a young animal gets"
                      : "The dose a young animal gets"}
                  </div>
                  {current.kidDoses.map((dose, index) => (
                    <div className="dose" key={dose.doseCode}>
                      <div className="dose-h">
                        <span className={index > 0 ? "dose-n bo" : "dose-n"}>
                          {index === 0
                            ? current.kidDoses.length > 1
                              ? "First dose"
                              : "Only dose"
                            : "Booster"}
                        </span>
                        <span className="lbl">
                          Give it when the animal is{" "}
                          <DurationField
                            days={dose.offsetDays}
                            title="Give this dose at"
                            disabled={!current.on}
                            onChange={(days) =>
                              updateVaccine(current.code, (v) => ({
                                ...v,
                                kidDoses: v.kidDoses.map((d, i) =>
                                  i === index ? { ...d, offsetDays: days } : d,
                                ),
                              }))
                            }
                          />{" "}
                          old
                        </span>
                      </div>
                      {index > 0 ? (
                        <p className="hintline" style={{ marginTop: 8 }}>
                          Counted from the animal&apos;s date of birth, the same as the first dose.
                        </p>
                      ) : null}
                    </div>
                  ))}
                </>
              ) : null}

              {current.on ? (
                <button
                  className="addrow"
                  type="button"
                  onClick={() =>
                    updateVaccine(current.code, (v) => ({
                      ...v,
                      kidDoses: [
                        ...v.kidDoses,
                        {
                          // A new dose starts three weeks after the last one, the
                          // minimum booster gap the safety rules enforce anyway.
                          offsetDays: (v.kidDoses.at(-1)?.offsetDays ?? 0) + 21,
                          triggerType: "birth_age",
                          doseCode: `new-kid-${v.kidDoses.length + 1}`,
                        },
                      ],
                    }))
                  }
                >
                  + Add a dose from date of birth
                </button>
              ) : null}

              {current.driveDoses.length > 0 ? (
                <>
                  <div className="sec-label" style={{ marginTop: 24 }}>
                    Doses given on a drive
                  </div>
                  {current.driveDoses.map((dose, index) => (
                    <div className="dose" key={dose.doseCode}>
                      <div className="dose-h">
                        <span className="dose-n">{index === 0 ? "First visit" : "Next visit"}</span>
                        <span className="lbl">
                          {index === 0 ? "Due " : "Then "}
                          <DurationField
                            days={dose.offsetDays}
                            title={index === 0 ? "Due after the drive starts" : "After the previous dose"}
                            disabled={!current.on}
                            onChange={(days) =>
                              updateVaccine(current.code, (v) => ({
                                ...v,
                                driveDoses: v.driveDoses.map((d, i) =>
                                  i === index ? { ...d, offsetDays: days } : d,
                                ),
                              }))
                            }
                          />{" "}
                          {index === 0 ? "after the drive starts" : "after the previous dose"}
                        </span>
                      </div>
                    </div>
                  ))}
                </>
              ) : null}

              {current.maxLateDays === null && current.on ? (
                <button
                  className="addrow"
                  type="button"
                  style={{ marginTop: 14 }}
                  onClick={() => updateVaccine(current.code, (v) => ({ ...v, maxLateDays: 7 }))}
                >
                  + Set how late a dose may be
                </button>
              ) : null}

              {current.maxLateDays !== null ? (
                <div className="dose" style={{ marginTop: 14 }}>
                  <div className="dose-h">
                    <span className="dose-n">Deadline</span>
                    <span className="lbl">
                      Any dose can be up to{" "}
                      <DurationField
                        days={current.maxLateDays}
                        title="Can be given up to … late"
                        disabled={!current.on}
                        onChange={(days) => updateVaccine(current.code, (v) => ({ ...v, maxLateDays: days }))}
                      />{" "}
                      late
                    </span>
                  </div>
                  <Timeline lateDays={current.maxLateDays} />
                </div>
              ) : null}

              <p className="hintline">
                The deadline runs forward from the due day only — giving a dose early does not buy
                extra time.
              </p>

              {current.repeatDays !== null ? (
                <div className="repeat">
                  <div className="rt">How often to repeat it</div>
                  <p className="rs">
                    Change this and every future vaccination date moves with it. No engineer needed.
                  </p>
                  <div className="sent">
                    <span className="lead">Repeat</span>
                    Do it again every{" "}
                    <DurationField
                      days={current.repeatDays}
                      title="Repeat every"
                      disabled={!current.on}
                      onChange={(days) => updateVaccine(current.code, (v) => ({ ...v, repeatDays: days }))}
                    />{" "}
                    after the last dose was given.
                  </div>
                  <div className="presets" role="group" aria-label="Common intervals">
                    {/* Values the picker can express exactly, so clicking a
                        preset and then reading the chip agree. */}
                    {[90, 180, 270, 365, 1095].map((days) => (
                      <button
                        className="preset"
                        key={days}
                        type="button"
                        aria-pressed={current.repeatDays === days}
                        disabled={!current.on}
                        onClick={() => updateVaccine(current.code, (v) => ({ ...v, repeatDays: days }))}
                      >
                        {humanDays(days)}
                      </button>
                    ))}
                  </div>
                </div>
              ) : (
                <>
                  <p className="hintline">This vaccine does not repeat.</p>
                  {current.on ? (
                    <button
                      className="addrow"
                      type="button"
                      onClick={() => updateVaccine(current.code, (v) => ({ ...v, repeatDays: 365 }))}
                    >
                      + Make it repeat
                    </button>
                  ) : null}
                </>
              )}
            </div>
          </section>

          <ProofCard mode={plan.proofMode} />
          <ProcurementCard plan={plan} setPlan={setPlan} onEdit={() => setSaved(false)} />
          <SafetyCard plan={plan} />
          <ImpactCard impact={props.impact} />
        </div>
      </div>

      <div className="actionbar">
        <div className="ab-in">
          <span className="pill">
            <span className="dot" />
            {props.draftLabel} · draft
          </span>
          <span className={saved && !dirty ? "saved-note show" : "saved-note"}>Draft saved.</span>
          {blockedReason ? <span className="ab-block">{blockedReason}</span> : null}
          {!blockedReason && props.cannotPublishReason ? (
            <span className="ab-block">{props.cannotPublishReason}</span>
          ) : null}
          <span className="ab-spacer" />
          <button
            className="btn ghost sm"
            type="button"
            disabled={pending || !dirty}
            onClick={() => {
              setPlan(props.initialPlan);
              setSaved(false);
            }}
          >
            Reset
          </button>
          <button
            className="btn"
            type="button"
            disabled={pending || !dirty || blockedReason !== null}
            title={blockedReason ?? undefined}
            onClick={onSave}
          >
            {pending ? "Working…" : "Save draft"}
          </button>
          <button
            className="btn pubb"
            type="button"
            disabled={pending || blockedReason !== null || !props.canPublish}
            title={blockedReason ?? props.cannotPublishReason ?? undefined}
            onClick={onPublish}
          >
            Publish plan
          </button>
        </div>
      </div>
    </div>
  );
}

/**
 * The deadline, drawn.
 *
 * Green is the due day, amber is the window in which a late dose is still
 * accepted. There is no red segment before the due day: the window runs forward
 * only, and drawing anything to the left would suggest a dose can be early.
 */
function Timeline({ lateDays }: { lateDays: number }) {
  return (
    <div className="tl">
      <div className="tl-bar">
        <div className="tl-seg g" style={{ flex: 1 }}>
          <span>Due day</span>
        </div>
        <div className="tl-seg a" style={{ flex: 3 }}>
          <span>Still accepted — up to {formatDays(lateDays)} late</span>
        </div>
      </div>
      <p className="tl-cap">
        After <b>{formatDays(lateDays)}</b> the task is missed and goes to Preventive Care review.
      </p>
    </div>
  );
}

function ProcurementCard({
  plan,
  setPlan,
  onEdit,
}: {
  plan: EditorPlan;
  setPlan: (fn: (p: EditorPlan) => EditorPlan) => void;
  onEdit: () => void;
}) {
  const { warmupNoVaccinationDays, kidsNormalScheduleUntilWeeks, adultPriorVaccinationAllowed } =
    plan.procurement;

  return (
    <section className="card">
      <div className="card-h">
        <div>
          <h2>Procurement holding</h2>
          <p className="s">
            Only vaccines given by us, in our parks or a supervised procurement holding park with
            proof, count as trusted history. Anything else starts the normal schedule once the
            animal reaches our sheds.
          </p>
        </div>
      </div>
      <div className="card-b">
        {warmupNoVaccinationDays !== null ? (
          <div className="sent">
            <span className="lead">Settle in</span>
            After it arrives, give no vaccine for{" "}
            <DurationField
              days={warmupNoVaccinationDays}
              title="Settle-in period"
              onChange={(days) => {
                onEdit();
                setPlan((p) => ({
                  ...p,
                  procurement: { ...p.procurement, warmupNoVaccinationDays: days },
                }));
              }}
            />
            .
          </div>
        ) : null}

        {kidsNormalScheduleUntilWeeks !== null ? (
          <div className="sent">
            <span className="lead">Kid or adult</span>
            Anything younger than{" "}
            <DurationField
              days={kidsNormalScheduleUntilWeeks * 7}
              title="Still counts as a kid until"
              onChange={(days) => {
                onEdit();
                setPlan((p) => ({
                  ...p,
                  procurement: {
                    ...p.procurement,
                    kidsNormalScheduleUntilWeeks: Math.max(1, Math.round(days / 7)),
                  },
                }));
              }}
            />{" "}
            follows the normal kid schedule. Older animals join the next drive.
          </div>
        ) : null}

        {adultPriorVaccinationAllowed !== null ? (
          <div className="sent">
            <span className="lead">Prior doses</span>
            Vaccinations the seller claims they already gave:
            <span className="seg" role="group" aria-label="Prior doses">
              <button
                className="segb"
                type="button"
                aria-pressed={adultPriorVaccinationAllowed}
                onClick={() => {
                  onEdit();
                  setPlan((p) => ({
                    ...p,
                    procurement: { ...p.procurement, adultPriorVaccinationAllowed: true },
                  }));
                }}
              >
                Count them
              </button>
              <button
                className="segb"
                type="button"
                aria-pressed={!adultPriorVaccinationAllowed}
                onClick={() => {
                  onEdit();
                  setPlan((p) => ({
                    ...p,
                    procurement: { ...p.procurement, adultPriorVaccinationAllowed: false },
                  }));
                }}
              >
                Ignore them
              </button>
            </span>
          </div>
        ) : null}
      </div>
    </section>
  );
}

/**
 * The rules that run whatever this plan says.
 *
 * Read-only and read from the document, not typed in here. A hardcoded list
 * would keep displaying "28 days" after someone changed the stored gap, which is
 * worse than showing nothing.
 */
function SafetyCard({ plan }: { plan: EditorPlan }) {
  const s = plan.safety;
  const lines: string[] = [];
  if (s.maxVaccinesPerSession) lines.push(`At most ${s.maxVaccinesPerSession} vaccines per animal per visit.`);
  if (s.liveToLiveGapDays) lines.push(`Live to live: at least ${humanDays(s.liveToLiveGapDays)} apart.`);
  if (s.liveToKilledGapDays && s.killedToKilledGapDays) {
    lines.push(
      `Live to killed and killed to killed: at least ${humanDays(s.liveToKilledGapDays)} apart unless the same-day rule allows it.`,
    );
  }
  if (s.kidBoosterMinGapDays) lines.push(`A booster is never closer than ${humanDays(s.kidBoosterMinGapDays)} to its first dose.`);
  if (s.skipFromPregnancyMonth && s.skipThroughPregnancyMonth) {
    lines.push(
      `Pregnancy months ${s.skipFromPregnancyMonth} and ${s.skipThroughPregnancyMonth} skip vaccination${
        s.postDeliveryCatchUpDays ? `; catch up within ${humanDays(s.postDeliveryCatchUpDays)} after delivery` : ""
      }.`,
    );
  }
  if (s.deferStates.length > 0) lines.push(`Deferred while: ${s.deferStates.join(", ").replace(/_/g, " ")}.`);
  if (s.maxBatchingHoldDays) {
    lines.push(`A drive may wait up to ${humanDays(s.maxBatchingHoldDays)} to batch; the safety window always wins.`);
  }

  if (lines.length === 0) return null;

  return (
    <section className="card">
      <div className="card-h">
        <div>
          <h2>
            Automatic safety rules <span className="ro">read-only</span>
          </h2>
          <p className="s">
            These are part of the plan and run after everything above, on every vaccine in it.
          </p>
        </div>
      </div>
      <div className="card-b">
        <ul className="safety">
          {lines.map((line) => (
            <li key={line}>
              <span className="sy"><Check size={10} aria-hidden /></span>
              {line}
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}

/**
 * How the operator proves it. Read-only here.
 *
 * The choice is stored on the version's proof policy, which the editor does not
 * write, so this states what is in force rather than offering a control that
 * would not save. Showing it matters: it decides what the phone asks for on
 * every vaccination task in this plan.
 */
function ProofCard({ mode }: { mode: "shed" | "animal" | null }) {
  if (!mode) return null;
  return (
    <section className="card">
      <div className="card-h">
        <div>
          <h2>
            How the operator proves it <span className="ro">read-only</span>
          </h2>
          <p className="s">
            One choice for the whole plan. It decides what the phone asks for on every vaccination
            task, whichever vaccine it is.
          </p>
        </div>
      </div>
      <div className="card-b">
        <div className="proofpick">
          <div className="pc" aria-pressed={mode === "shed"}>
            <span className="pc-h">
              <span className="pc-r" />
              <span className="pc-t">One video per shed</span>
            </span>
            <span className="pc-d">
              The operator scans every animal&apos;s tag as it is done, then records one video
              covering the whole shed. A 200-animal shed produces 1 clip.
            </span>
          </div>
          <div className="pc" aria-pressed={mode === "animal"}>
            <span className="pc-h">
              <span className="pc-r" />
              <span className="pc-t">One video per animal</span>
            </span>
            <span className="pc-d">
              The operator scans a tag and records that animal&apos;s injection, one at a time. A
              200-animal shed produces 200 clips.
            </span>
          </div>
        </div>
      </div>
    </section>
  );
}

/**
 * What publishing will change.
 *
 * Measured against the CURRENT herd by the backend's own eligibility rollup, so
 * it is the same arithmetic the drive planner uses rather than a second estimate
 * that could disagree with it. It describes the size of the work, not which
 * dates move -- that depends on each animal's own history.
 */
function ImpactCard({ impact }: { impact: ImpactSummary | null }) {
  if (!impact) return null;
  const overCap = impact.capacityStatus && impact.capacityStatus !== "within_cap";
  return (
    <section className="card">
      <div className="card-h">
        <div>
          <h2>What publishing will change</h2>
          <p className="s">
            Work already given, and any drive running right now, is untouched. Future dates are
            rebuilt from this plan.
          </p>
        </div>
      </div>
      <div className="card-b">
        <div className="impact">
          <div className="stat">
            <div className="n num">{impact.eligibleAnimals.toLocaleString("en-IN")}</div>
            <div className="l">animals in scope</div>
          </div>
          <div className="stat">
            <div className="n num">{impact.affectedSheds}</div>
            <div className="l">sheds affected</div>
          </div>
          <div className={overCap ? "stat warn" : "stat"}>
            <div className="n num">{impact.estimatedDays}</div>
            <div className="l">operator-days at {impact.dailyCap}/day</div>
          </div>
        </div>
        {overCap ? (
          <div className="spike" style={{ marginTop: 14 }}>
            <span className="sx">!</span>
            <span>
              This does not fit in one day at the current cap of{" "}
              <b>{impact.dailyCap}</b> animals. It will be split across{" "}
              <b>{impact.estimatedDays}</b> days inside the safe window.
            </span>
          </div>
        ) : null}
      </div>
    </section>
  );
}

function initials(name: string): string {
  const cleaned = name.replace(/[^A-Za-z0-9+ ]/g, " ").trim();
  const parts = cleaned.split(/[\s+]+/).filter(Boolean);
  if (parts.length >= 2) return `${parts[0][0]}${parts[1][0]}`.toUpperCase();
  return cleaned.slice(0, 2).toUpperCase();
}

function summarise(v: EditorVaccine): string {
  if (!v.on) return "Not in this plan";
  const bits: string[] = [];
  if (v.kidDoses.length > 0) bits.push(`from ${formatDays(v.kidDoses[0].offsetDays)}`);
  if (v.driveDoses.length > 0) bits.push(`${v.driveDoses.length} on a drive`);
  if (v.repeatDays !== null) bits.push(`repeats every ${humanDays(v.repeatDays)}`);
  return bits.join(" · ") || "In this plan";
}
