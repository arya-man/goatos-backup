// The Feed Config session-plan form: a park's feeding sessions, each a name and a share of the day.
//
// The author types a PERCENTAGE ("60") because that is how the farm talks about the split; the API
// stores a FRACTION with four places ("0.6000"). The conversion is done in integer basis points on
// the string, never through a float, so 33.33 stays 0.3333 and never becomes 0.33329999.

export type SessionPlanRow = { session_no: number; session_label: string; split_fraction: string };

export type SessionPlanParse =
  | { ok: true; sessions: SessionPlanRow[] }
  | { ok: false; messageKey: string };

/** "60" -> "0.6000", "33.33" -> "0.3333", "100" -> "1.0000". null for anything that is not a share
 * from 0.01 to 100 with at most two decimal places. */
export function percentToFraction(raw: string): string | null {
  const text = raw.trim();
  const match = /^(\d{1,3})(?:\.(\d{1,2}))?$/.exec(text);
  if (!match) return null;
  const basisPoints = Number(match[1]) * 100 + Number((match[2] ?? "").padEnd(2, "0") || "0");
  if (basisPoints < 1 || basisPoints > 10000) return null;
  return `${Math.floor(basisPoints / 10000)}.${String(basisPoints % 10000).padStart(4, "0")}`;
}

/** "0.6000" -> "60", "0.3333" -> "33.33": the inverse, for pre-filling the form. */
export function fractionToPercent(fraction: string): string {
  const match = /^(\d+)(?:\.(\d{0,4}))?$/.exec(fraction.trim());
  if (!match) return "";
  const basisPoints = Number(match[1]) * 10000 + Number((match[2] ?? "").padEnd(4, "0") || "0");
  const whole = Math.floor(basisPoints / 100);
  const rest = basisPoints % 100;
  return rest === 0 ? String(whole) : `${whole}.${String(rest).padStart(2, "0").replace(/0$/, "")}`;
}

/**
 * Reads the form rows (session_no_N, session_label_N, session_share_N for N < session_count). A row
 * whose name is blank is a session being REMOVED (or an unused blank row) and is left out. The
 * shares of the kept rows must add up to exactly 100%; the server checks the same rule.
 */
export function parseSessionPlanForm(get: (name: string) => string): SessionPlanParse {
  const count = Number(get("session_count"));
  if (!Number.isInteger(count) || count < 0 || count > 16) return { ok: false, messageKey: "action.sessions_rejected" };
  const sessions: SessionPlanRow[] = [];
  let total = 0;
  for (let i = 0; i < count; i += 1) {
    const label = get(`session_label_${i}`).trim().replace(/\s+/g, " ");
    if (!label) continue;
    const sessionNo = Number(get(`session_no_${i}`));
    if (!Number.isInteger(sessionNo) || sessionNo < 1) return { ok: false, messageKey: "action.sessions_rejected" };
    const fraction = percentToFraction(get(`session_share_${i}`));
    if (!fraction) return { ok: false, messageKey: "reason.session_share_invalid" };
    total += Math.round(Number(fraction) * 10000);
    sessions.push({ session_no: sessionNo, session_label: label, split_fraction: fraction });
  }
  if (sessions.length === 0) return { ok: false, messageKey: "reason.sessions_required" };
  if (total !== 10000) return { ok: false, messageKey: "reason.session_split_not_whole" };
  return { ok: true, sessions };
}
