#!/usr/bin/env node
// Lane 6 (delta) — the transition checks.
//
// Each check reads TWO snapshots and asks whether the change between them was legal. Every one
// is derived from a rule already written in AGENTS.md or a decision doc; none is invented here,
// and each names the rule it came from so a reader can go and disagree with the rule rather than
// with the check.
//
// These are pure functions over two JSON readings. No database, no browser, no clock of their
// own — the snapshot carries the farm's own day. That is deliberate: a check that cannot be run
// without production is a check nobody mutation-proves.
//
// THREE RULES BIND EVERY CHECK HERE, and each one is a way a check passes against nothing:
//   1. A check counts what it READ. `examined` is the number of real things compared, and a
//      check that examined nothing reports "nothing to compare", never "pass".
//   2. A missing input is never "nothing changed". If a section a check needs was not read in
//      either snapshot, the check reports "not checked" and names the missing reading.
//   3. A finding is one-sided where the rule is one-sided. A check that can only fire on the
//      direction the rule forbids cannot cry wolf on the direction it allows.

/** The cutoff after which a low-priority pen move is planned work for the day AFTER tomorrow. */
export const SHIFTING_CUTOFF_IST = "13:30";

function section(snapshot, name) {
  return snapshot?.sections?.[name] ?? null;
}

/**
 * The rows of a section, or a stated reason there are none. A section that was not read, or was
 * read only in part, yields NO rows and a reason — it never yields an empty list, because an
 * empty list is indistinguishable from a quiet farm.
 */
export function readSection(snapshot, name, which) {
  const found = section(snapshot, name);
  if (!found) return { ok: false, reason: `${which}'s reading of ${name.replaceAll("_", " ")} is missing` };
  if (found.read !== true) return { ok: false, reason: `${which}'s reading of ${name.replaceAll("_", " ")} could not be taken` };
  if (found.capped === true) return { ok: false, reason: `${which}'s reading of ${name.replaceAll("_", " ")} was incomplete, so it cannot be compared` };
  return { ok: true, rows: found.rows ?? [] };
}

/** Gathers the sections a check needs from both snapshots, or the first reason it cannot. */
export function gather(before, after, names) {
  const out = { before: {}, after: {} };
  for (const name of names) {
    const b = readSection(before, name, "yesterday");
    if (!b.ok) return { ok: false, reason: b.reason };
    const a = readSection(after, name, "today");
    if (!a.ok) return { ok: false, reason: a.reason };
    out.before[name] = b.rows;
    out.after[name] = a.rows;
  }
  return { ok: true, ...out };
}

function index(rows, keyOf) {
  const map = new Map();
  for (const row of rows) map.set(keyOf(row), row);
  return map;
}

function group(rows, keyOf) {
  const map = new Map();
  for (const row of rows) {
    const key = keyOf(row);
    if (!map.has(key)) map.set(key, []);
    map.get(key).push(row);
  }
  return map;
}

function num(value) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : null;
}

function addDays(date, days) {
  const base = new Date(`${date}T00:00:00Z`);
  base.setUTCDate(base.getUTCDate() + Number(days));
  return base.toISOString().slice(0, 10);
}

/**
 * The feed day a sale's reduction is owed on. The park's OWN correction clock decides it — never
 * a constant. Tagged before the cutoff, the next day's sheet can still be corrected; at or after
 * it, the next day's sheet is already batched and the day after is the first one that can carry
 * the change. A park with no clock on record falls back to the next day, exactly as the farm's
 * own rule does.
 */
export function saleFeedReductionDay(soldOn, soldAtTime, correctionTime) {
  if (!soldOn) return null;
  if (!correctionTime || !soldAtTime) return addDays(soldOn, 1);
  return soldAtTime < correctionTime ? addDays(soldOn, 1) : addDays(soldOn, 2);
}

/**
 * The first business day a pen move raised at `raisedAtTime` may be counted on. One-sided on
 * purpose: this is the EARLIEST legal day, so a move planned further out is not an offence.
 */
export function earliestShiftingEffectiveDay(raisedOn, raisedAtTime) {
  if (!raisedOn || !raisedAtTime) return null;
  return addDays(raisedOn, raisedAtTime < SHIFTING_CUTOFF_IST ? 1 : 2);
}

const OPEN_WEIGHING_STATES = new Set(["in_progress", "submitted", "pending_verification", "verification_pending"]);

export const DELTA_CHECKS = [
  {
    name: "work_left_open_against_an_animal_that_left",
    question: "When an animal leaves the herd, is its outstanding vaccination work cleared the next day?",
    rule: "AGENTS.md: vaccination obligations belong to live animals; lane 2 already finds them standing against sold and dead animals, and this asks whether the farm gets them cleared once it has had a day to.",
    severity: "high",
    page: { title: "Vaccination", path: "/vaccination" },
    countUnit: "animals that left the herd and still have vaccinations booked",
    needs: ["exited_animals_open_vaccination"],
    run(before, after) {
      const got = gather(before, after, this.needs);
      if (!got.ok) return { notChecked: got.reason };
      const yesterday = index(got.before.exited_animals_open_vaccination, (row) => row.animal);
      const breaches = [];
      let examined = 0;
      for (const row of got.after.exited_animals_open_vaccination) {
        const stillOpen = num(row.still_open);
        if (stillOpen === null) continue;
        const wasThere = yesterday.get(row.animal);
        // Only an animal whose departure was ALREADY on the books yesterday: the farm has had a
        // full day to clear the work. An animal that left this morning is not yet late.
        if (!wasThere) continue;
        if (num(wasThere.still_open) === null) continue;
        examined += 1;
        if (stillOpen > 0) {
          breaches.push({
            sentence: `Animal ${row.animal_label} left the herd on ${row.left_on} and still has ${stillOpen} vaccination${stillOpen === 1 ? "" : "s"} booked against it a day later.`,
            where: "Vaccination"
          });
        }
      }
      return { examined, breaches };
    }
  },
  {
    name: "a_pen_that_lost_animals_to_a_sale_was_never_re_fed",
    question: "After animals are tagged to a sale, does that pen get a feed sheet on the day the farm's own correction clock says the reduction lands?",
    rule: "docs/decisions/sale-feed-reduce-notification.md and feeddirection/domain.SaleFeedReductionDay: before the park's correction cutoff the reduction lands on the next day, at or after it on the day after — never a constant.",
    severity: "high",
    page: { title: "Feed", path: "/feed/analytics" },
    countUnit: "pens sold out of with no feed sheet on the day the reduction was owed",
    needs: ["sale_allocations", "park_feed_clocks", "feed_pen_head_counts"],
    run(before, after) {
      const got = gather(before, after, this.needs);
      if (!got.ok) return { notChecked: got.reason };
      const clocks = new Map();
      for (const row of got.after.park_feed_clocks) {
        if (row.workflow === "normal") clocks.set(`${row.farm}|${row.park}`, row.corrects_at);
      }
      const sheets = new Set(got.after.feed_pen_head_counts.map((row) => `${row.farm}|${row.park}|${row.pen_shed}|${row.feed_day}`));
      // The feed days this reading actually covers. A day outside the window was not read, so a
      // missing sheet for it is not evidence of anything.
      const days = got.after.feed_pen_head_counts.map((row) => row.feed_day).filter(Boolean).sort();
      const covered = days.length ? { from: days[0], to: days[days.length - 1] } : null;
      if (!covered) return { examined: 0, breaches: [] };
      const breaches = [];
      let examined = 0;
      for (const sale of got.after.sale_allocations) {
        const owedOn = saleFeedReductionDay(sale.sold_on, sale.sold_at_time, clocks.get(`${sale.farm}|${sale.park}`));
        if (!owedOn || owedOn < covered.from || owedOn > covered.to) continue;
        examined += 1;
        if (!sheets.has(`${sale.farm}|${sale.park}|${sale.pen_shed}|${owedOn}`)) {
          breaches.push({
            sentence: `${sale.animals} animal${sale.animals === "1" ? "" : "s"} were tagged to a sale out of a pen on ${sale.sold_on}, and that pen has no feed sheet at all for ${owedOn}, the day its feed should have come down.`,
            where: "Feed"
          });
        }
      }
      return { examined, breaches };
    }
  },
  {
    name: "a_pen_sold_out_of_is_still_fed_for_the_animals_that_left",
    question: "After animals leave a pen for a sale, and nothing arrives to replace them, does the pen's feed sheet stop being written for more mouths than before?",
    rule: "The same sale rule, read on the number rather than the existence of the sheet. Guarded by the pen's own living count, so a pen that genuinely gained animals is never accused.",
    severity: "high",
    page: { title: "Feed", path: "/feed/analytics" },
    countUnit: "pens still fed for more mouths after animals left them",
    needs: ["sale_allocations", "park_feed_clocks", "feed_pen_head_counts", "pen_live_counts"],
    run(before, after) {
      const got = gather(before, after, this.needs);
      if (!got.ok) return { notChecked: got.reason };
      const clocks = new Map();
      for (const row of got.after.park_feed_clocks) {
        if (row.workflow === "normal") clocks.set(`${row.farm}|${row.park}`, row.corrects_at);
      }
      const mouths = new Map();
      for (const row of got.after.feed_pen_head_counts) {
        mouths.set(`${row.farm}|${row.park}|${row.pen_shed}|${row.feed_day}`, num(row.mouths));
      }
      // A pen that GAINED living animals between the two readings has an innocent explanation for
      // a higher sheet — a move in, a birth — so it is never accused. Counts are summed over the
      // pens inside a shed, because the feed sheet is written per shed.
      const livingBefore = new Map();
      const livingAfter = new Map();
      for (const [rows, into] of [[got.before.pen_live_counts, livingBefore], [got.after.pen_live_counts, livingAfter]]) {
        for (const row of rows) {
          const key = `${row.farm}|${row.park}|${row.pen_shed}`;
          into.set(key, (into.get(key) ?? 0) + (num(row.living) ?? 0));
        }
      }
      const breaches = [];
      let examined = 0;
      for (const sale of got.after.sale_allocations) {
        const owedOn = saleFeedReductionDay(sale.sold_on, sale.sold_at_time, clocks.get(`${sale.farm}|${sale.park}`));
        if (!owedOn) continue;
        const penKey = `${sale.farm}|${sale.park}|${sale.pen_shed}`;
        const beforeDay = addDays(sale.sold_on, 0);
        const was = mouths.get(`${penKey}|${beforeDay}`);
        const now = mouths.get(`${penKey}|${owedOn}`);
        if (was === null || was === undefined || now === null || now === undefined) continue;
        const gained = (livingAfter.get(penKey) ?? 0) > (livingBefore.get(penKey) ?? 0);
        if (gained) continue;
        examined += 1;
        if (now > was) {
          breaches.push({
            sentence: `A pen that ${sale.animals} animal${sale.animals === "1" ? "" : "s"} left on ${sale.sold_on} is fed for ${now} mouths on ${owedOn}, up from ${was}, and no animal arrived in it.`,
            where: "Feed"
          });
        }
      }
      return { examined, breaches };
    }
  },
  {
    name: "a_pen_move_raised_late_is_treated_as_tomorrows_work",
    question: "Is a pen move raised after the afternoon cutoff held until the day after tomorrow, rather than counted for tomorrow?",
    rule: "AGENTS.md shifting lead time and counts/domain.ShiftingActionsDueFrom: low priority raised before 13:30 IST is due the next day; raised at or after, the day after. High priority is due at once and is not judged here.",
    severity: "high",
    page: { title: "Herd operations", path: "/counts/shifting" },
    countUnit: "pen moves counted a day earlier than the cutoff allows",
    needs: ["shifting_raised"],
    run(before, after) {
      const got = gather(before, after, this.needs);
      if (!got.ok) return { notChecked: got.reason };
      const seen = new Set(got.before.shifting_raised.map((row) => row.move));
      const breaches = [];
      let examined = 0;
      for (const row of got.after.shifting_raised) {
        if (seen.has(row.move)) continue;
        if (String(row.priority).toLowerCase() === "high") continue;
        const earliest = earliestShiftingEffectiveDay(row.raised_on, row.raised_at_time);
        if (!earliest || !row.effective_on) continue;
        examined += 1;
        // One-sided. A move planned further out than the rule's earliest day is ordinary
        // planning; only a move counted EARLIER than the cutoff allows is an offence.
        if (row.effective_on < earliest) {
          breaches.push({
            sentence: `A pen move raised at ${row.raised_at_time} on ${row.raised_on} is being counted from ${row.effective_on}, but work raised that late is not owed until ${earliest}.`,
            where: "Herd operations"
          });
        }
      }
      return { examined, breaches };
    }
  },
  {
    name: "a_weighing_handed_in_reached_nobody_to_review_it",
    question: "When a weighing job is handed in, does a video reach the reviewer?",
    rule: "AGENTS.md weighing: a submitted weighing raises one verification item per piece of evidence; the reviewer is how the job later closes.",
    severity: "high",
    page: { title: "Weighing", path: "/weighing/weights" },
    countUnit: "weighing jobs handed in with nothing for the reviewer",
    needs: ["weighing_buckets", "weighing_review_items"],
    run(before, after) {
      const got = gather(before, after, this.needs);
      if (!got.ok) return { notChecked: got.reason };
      const was = index(got.before.weighing_buckets, (row) => row.job);
      const reviews = index(got.after.weighing_review_items, (row) => row.job);
      const breaches = [];
      let examined = 0;
      for (const row of got.after.weighing_buckets) {
        const previously = was.get(row.job);
        if (!previously) continue;
        const handedInNow = OPEN_WEIGHING_STATES.has(row.state) && row.state !== "in_progress";
        const handedInBefore = OPEN_WEIGHING_STATES.has(previously.state) && previously.state !== "in_progress";
        if (!handedInNow || handedInBefore) continue;
        examined += 1;
        const item = reviews.get(row.job);
        if (!item || num(item.altogether) === 0) {
          breaches.push({
            sentence: `The weighing of ${row.pen_label} was handed in, and no video reached the reviewer.`,
            where: "Weighing"
          });
        }
      }
      return { examined, breaches };
    }
  },
  {
    name: "a_weighing_closed_while_a_video_was_still_waiting",
    question: "Did a weighing job close while one of its videos was still waiting on the reviewer?",
    rule: "AGENTS.md weighing: the close gate is unconditional — a bucket cannot close while verification is pending, and there is no caller-supplied way past it.",
    severity: "high",
    page: { title: "Weighing", path: "/weighing/weights" },
    countUnit: "weighing jobs closed with a video still waiting",
    needs: ["weighing_buckets", "weighing_review_items"],
    run(before, after) {
      const got = gather(before, after, this.needs);
      if (!got.ok) return { notChecked: got.reason };
      const was = index(got.before.weighing_buckets, (row) => row.job);
      const reviews = index(got.after.weighing_review_items, (row) => row.job);
      const breaches = [];
      let examined = 0;
      for (const row of got.after.weighing_buckets) {
        const previously = was.get(row.job);
        if (!previously) continue;
        if (row.state !== "closed" || previously.state === "closed") continue;
        const item = reviews.get(row.job);
        if (!item) continue;
        const waiting = num(item.waiting);
        if (waiting === null) continue;
        examined += 1;
        if (waiting > 0) {
          breaches.push({
            sentence: `The weighing of ${row.pen_label} was closed with ${waiting} video${waiting === 1 ? "" : "s"} still waiting on the reviewer.`,
            where: "Weighing"
          });
        }
      }
      return { examined, breaches };
    }
  },
  {
    name: "a_feed_load_that_arrived_owes_a_strip_test_and_has_none",
    question: "Does every feed load recorded as arrived get an aflatoxin test round?",
    rule: "AGENTS.md toxin module: every feed load recorded on /procurement/feed-purchases owes one strip test, born automatically when the load is marked reached — never hand-created.",
    severity: "high",
    page: { title: "Procurement", path: "/procurement/feed-purchases" },
    countUnit: "feed loads that arrived with no strip test",
    needs: ["feed_loads_reached", "toxin_rounds"],
    run(before, after) {
      const got = gather(before, after, this.needs);
      if (!got.ok) return { notChecked: got.reason };
      const seen = new Set(got.before.feed_loads_reached.map((row) => row.load));
      const rounds = index(got.after.toxin_rounds, (row) => row.load);
      const breaches = [];
      let examined = 0;
      for (const row of got.after.feed_loads_reached) {
        // A load that only arrived on the record today may still be having its round written.
        // A load that was already on the record yesterday has had a full day.
        if (!seen.has(row.load)) continue;
        examined += 1;
        const found = rounds.get(row.load);
        if (!found || num(found.rounds) === 0) {
          breaches.push({
            sentence: `A load of ${row.feed} recorded as arriving at ${row.farm_label} on ${row.arrived_on} has no aflatoxin test against it a day later.`,
            where: "Procurement"
          });
        }
      }
      return { examined, breaches };
    }
  },
  {
    name: "a_pen_worked_yesterday_is_owed_a_visit_and_has_none",
    question: "The day after preventive-care work is handed in for a pen, does that pen have a visit owed to one of the farm's named visitors?",
    rule: "AGENTS.md pen visit tasks: the day after any vaccination shed proof or PC Care task is submitted in a pen, one of the park's configured visitors owes that pen a visit; a park with no configured visitor gets no task and a loud log, never a fallback person.",
    severity: "high",
    page: { title: "Tasks", path: "/pen-visits" },
    countUnit: "pens worked in with no visit owed the next day",
    needs: ["pen_care_submitted", "pen_visit_tasks", "pen_visit_visitors"],
    run(before, after) {
      const got = gather(before, after, this.needs);
      if (!got.ok) return { notChecked: got.reason };
      const parksWithVisitors = new Set(got.after.pen_visit_visitors.map((row) => `${row.farm}|${row.park}`));
      const tasks = index(got.after.pen_visit_tasks, (row) => `${row.farm}|${row.park}|${row.pen_shed}|${row.worked_on}`);
      const breaches = [];
      let examined = 0;
      for (const row of got.after.pen_care_submitted) {
        // Only work handed in strictly before today, so the day-after task has had its day.
        if (!row.worked_on || row.worked_on >= after.businessDate) continue;
        // A park the farm has named nobody for is deliberately owed no visit. Silence there is
        // the rule working, not a breach.
        if (!parksWithVisitors.has(`${row.farm}|${row.park}`)) continue;
        examined += 1;
        const task = tasks.get(`${row.farm}|${row.park}|${row.pen_shed}|${row.worked_on}`);
        // WHO the visit falls to is deliberately not judged here: the task carries no named
        // person any more, because any of the park's named visitors may go. What is judged is
        // that the visit is owed at all, in a park that has somebody to owe it to.
        if (!task) {
          breaches.push({
            sentence: `Preventive-care work was handed in for a pen on ${row.worked_on} and nobody is owed a visit to that pen.`,
            where: "Tasks"
          });
        }
      }
      return { examined, breaches };
    }
  },
  {
    name: "a_vaccination_round_went_backwards",
    question: "Did a vaccination round report less work done today than it did yesterday?",
    rule: "AGENTS.md vaccination progress: field work done is completed plus submitted and the backend owns the single number. Work an operator has already done cannot become undone.",
    severity: "high",
    page: { title: "Vaccination", path: "/vaccination" },
    countUnit: "vaccination rounds that went backwards overnight",
    needs: ["drive_progress"],
    run(before, after) {
      const got = gather(before, after, this.needs);
      if (!got.ok) return { notChecked: got.reason };
      const was = index(got.before.drive_progress, (row) => `${row.farm}|${row.round}|${row.planned_for}`);
      const breaches = [];
      let examined = 0;
      for (const row of got.after.drive_progress) {
        const previously = was.get(`${row.farm}|${row.round}|${row.planned_for}`);
        if (!previously) continue;
        const nowDone = num(row.done);
        const wasDone = num(previously.done);
        if (nowDone === null || wasDone === null) continue;
        examined += 1;
        if (nowDone < wasDone) {
          breaches.push({
            sentence: `A vaccination round planned for ${row.planned_for} counted ${wasDone} animals done yesterday and counts ${nowDone} today.`,
            where: "Vaccination"
          });
        }
      }
      return { examined, breaches };
    }
  },
  {
    name: "an_open_job_was_moved_onto_a_newly_published_procedure",
    question: "When a written procedure is republished, does work already underway stay on the version it started on?",
    rule: "AGENTS.md SOP-driven herd operations: publishing a new version changes the NEXT workflow opened; a workflow already open keeps the steps it started with.",
    severity: "high",
    page: { title: "Herd operations", path: "/counts/sops" },
    countUnit: "jobs underway that changed procedure mid-flight",
    needs: ["open_workflow_sop_pins"],
    run(before, after) {
      const got = gather(before, after, this.needs);
      if (!got.ok) return { notChecked: got.reason };
      const was = index(got.before.open_workflow_sop_pins, (row) => row.workflow);
      const breaches = [];
      let examined = 0;
      for (const row of got.after.open_workflow_sop_pins) {
        const previously = was.get(row.workflow);
        if (!previously) continue;
        // Only a job that carried a version yesterday and carries one today can have changed
        // version; a job that carried none is a different question and not this check's.
        if (!previously.procedure_version || !row.procedure_version) continue;
        examined += 1;
        if (previously.procedure_version !== row.procedure_version) {
          breaches.push({
            sentence: `A ${String(row.kind).replaceAll(".", " ")} already underway when it was opened on ${row.opened_on} is now being worked to a different version of the written procedure.`,
            where: "Herd operations"
          });
        }
      }
      return { examined, breaches };
    }
  },
  {
    name: "the_herd_total_moved_without_anything_leaving_or_arriving",
    question: "Does the number of living animals only move by animals arriving or leaving?",
    rule: "AGENTS.md herd register: an animal leaves by a terminal exit and arrives by a birth or a purchase. A living total that moves by more than the departures on record has lost or gained animals nobody recorded.",
    severity: "high",
    page: { title: "Herd register", path: "/counts/herd" },
    countUnit: "farms whose living total fell by more than the animals recorded as leaving",
    needs: ["living_total", "exited_animals_open_vaccination"],
    run(before, after) {
      const got = gather(before, after, this.needs);
      if (!got.ok) return { notChecked: got.reason };
      const was = index(got.before.living_total, (row) => row.farm);
      const leftToday = new Map();
      const leftBefore = new Set(got.before.exited_animals_open_vaccination.map((row) => row.animal));
      for (const row of got.after.exited_animals_open_vaccination) {
        if (leftBefore.has(row.animal)) continue;
        leftToday.set(row.farm, (leftToday.get(row.farm) ?? 0) + 1);
      }
      const breaches = [];
      let examined = 0;
      for (const row of got.after.living_total) {
        const previously = was.get(row.farm);
        if (!previously) continue;
        const now = num(row.living);
        const before2 = num(previously.living);
        if (now === null || before2 === null) continue;
        examined += 1;
        const fell = before2 - now;
        const recorded = leftToday.get(row.farm) ?? 0;
        // One-sided. A total that ROSE is births and purchases, which this reading does not
        // carry, so it is never an offence here. Only a fall larger than the departures on
        // record is animals that vanished without a record.
        if (fell > recorded) {
          breaches.push({
            sentence: `The farm held ${fell} fewer living animals today than yesterday, and only ${recorded} ${recorded === 1 ? "is" : "are"} recorded as having left.`,
            where: "Herd register"
          });
        }
      }
      return { examined, breaches };
    }
  }
];

export function checkByName(name) {
  return DELTA_CHECKS.find((check) => check.name === name) ?? null;
}
