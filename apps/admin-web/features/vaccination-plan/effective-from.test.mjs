import assert from "node:assert/strict";
import test from "node:test";

import { todayIso } from "../../lib/format.ts";

/**
 * The date a published plan says it took effect.
 *
 * The server's own calendar day is the wrong one to ask. Cloud Run runs in UTC, so between
 * 00:00 and 05:29 IST the server is still on YESTERDAY -- and a CEO publishing early in the
 * morning would stamp the plan as effective from the day before, reading as a plan already
 * in force before anyone approved it.
 *
 * `plan-actions.ts` is "use server" and cannot be imported here, so this asserts the shared
 * business-day source it now uses, under exactly that clock.
 */

function withClock(iso, fn) {
  const RealDate = Date;
  const fixed = new RealDate(iso);
  // eslint-disable-next-line no-global-assign
  Date = class extends RealDate {
    constructor(...args) {
      return args.length ? new RealDate(...args) : new RealDate(fixed);
    }
    static now() {
      return fixed.getTime();
    }
  };
  try {
    return fn();
  } finally {
    // eslint-disable-next-line no-global-assign
    Date = RealDate;
  }
}

test("the business date is India's, not the server's, once India has rolled over", () => {
  // 22:00 UTC on the 21st is 03:30 IST on the 22nd. The farm's day is the 22nd.
  const got = withClock("2026-08-21T22:00:00Z", () => todayIso());
  assert.equal(got, "2026-08-22", "a plan published at 03:30 IST must take effect that day, not the day before");
});

test("and it does not run ahead of India either", () => {
  // 17:00 UTC on the 21st is 22:30 IST the same day.
  const got = withClock("2026-08-21T17:00:00Z", () => todayIso());
  assert.equal(got, "2026-08-21");
});

test("effective_from is midnight of that business day, in the shape the backend parses", () => {
  const effectiveFrom = withClock("2026-08-21T22:00:00Z", () => `${todayIso()}T00:00:00Z`);
  assert.equal(effectiveFrom, "2026-08-22T00:00:00Z");
  assert.ok(!Number.isNaN(Date.parse(effectiveFrom)), "must be RFC3339 the Go backend accepts");
});
