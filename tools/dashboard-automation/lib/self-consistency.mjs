// A screen's figures must agree with each other — on EVERY screen, not the one the
// bug was reported on.
//
// The per-endpoint relations in api-contract-checks.json say what a particular page
// promises. This file is the other half: rules that are true of ANY answer from ANY
// endpoint, found by the shape of the answer rather than by naming fields one page at
// a time. It needs no configuration, so it sweeps every route the catalogue carries,
// including ones nobody has written a relation for yet.
//
// The bar for a rule living here is that it must be wrong EVERYWHERE, with no farm
// and no season in which it is a legitimate answer:
//   - a SHARE expressed as a percentage, outside 0 to 100
//   - a count below zero
//   - an average that is not its own total over its own head count
//   - a stated total that disagrees with the list printed under it
//
// One rule was tried here and taken back out: "the same row listed twice". Run against
// all 58 catalogue endpoints it fired on 20 of them and every single one was wrong. It
// had guessed that a field ending in id identifies the row, when park id, shed id,
// tenant id and operator id are the row's FOREIGN keys -- many rows share a farm, a pen
// and a person, and that is what those lists are for. Row identity cannot be worked out
// from the shape of an answer; the page has to say what one row is. It therefore lives
// in api-contract-checks.json as a declared relation, per endpoint, with the identity
// named and checked against what production returns. Left here it would have put a red
// cross on twenty correct pages, and a check nobody believes is worse than no check.
//
// Anything narrower than that belongs in a per-endpoint relation, where the page can
// say what it means. A rule here that is merely usually true would fire on correct
// pages across the whole product at once, and a check nobody trusts is worse than no
// check at all.

const COUNT_WORDS = /(^|_)(count|total|animals|head|doses|rows|items)($|_)|_count$|Count$|_total$|Total$/;
const PERCENT_WORDS = /percent|_pct$|Pct$|share_of/i;
// A share of something is 0 to 100. A CHANGE is not a share: herd signals really does
// report motion up 272% on one pen and down 61% on another, and both are true. Only
// shares are held to the range.
const CHANGE_WORDS = /delta|change|diff|growth|trend|movement|vs_|_vs|swing|variance|gain|drop|rise|shift/i;
const TRUNCATED_WORDS = /truncat/i;

const isObject = (value) => value !== null && typeof value === "object" && !Array.isArray(value);
const num = (value) => (typeof value === "number" && Number.isFinite(value) ? value : null);

/** Walks the answer and reports the disagreements it finds, each with a plain sentence. */
export function selfConsistencyFindings(payload, { pageName = "this screen", maxNodes = 20000 } = {}) {
  const findings = [];
  const seenRule = new Set();
  let visited = 0;
  const add = (rule, where, sentence) => {
    const key = `${rule}|${where}`;
    if (seenRule.has(key)) return;
    seenRule.add(key);
    findings.push({ rule, where, sentence });
  };

  const walk = (value, trail) => {
    if (visited++ > maxNodes) return;
    if (Array.isArray(value)) {
      value.forEach((item, index) => walk(item, `${trail}[${index}]`));
      return;
    }
    if (!isObject(value)) return;

    for (const [key, item] of Object.entries(value)) {
      const here = trail ? `${trail}.${key}` : key;
      const n = num(item);
      if (n !== null) {
        // A percentage outside nought to a hundred is wrong on every screen there is.
        if (PERCENT_WORDS.test(key) && !CHANGE_WORDS.test(key) && (n < 0 || n > 100)) {
          add("impossible-percentage", here, `${pageName} shows a percentage of ${n}, which is not a percentage anything can be.`);
        }
        // Nothing countable is ever a negative number of things.
        if (COUNT_WORDS.test(key) && n < 0 && !/delta|change|gain|diff|balance|variance|adg|movement/i.test(key)) {
          add("negative-count", here, `${pageName} counts ${n} of something, and nothing can be counted a negative number of times.`);
        }
      }

      // An average beside its own total and its own head count.
      const avg = /^(average|avg|mean)_?(.*)$/i.exec(key);
      if (avg && num(item) !== null) {
        const suffix = avg[2];
        const totalKey = Object.keys(value).find((k) => k !== key && /^(total|sum)_?/i.test(k) && (suffix ? k.toLowerCase().includes(suffix.toLowerCase().replace(/^_/, "").split("_")[0]) : true));
        const countKey = Object.keys(value).find((k) => k !== key && k !== totalKey && COUNT_WORDS.test(k) && !/percent|total|weight|kg/i.test(k));
        const total = totalKey ? num(value[totalKey]) : null;
        const count = countKey ? num(value[countKey]) : null;
        if (total !== null && count !== null && count > 0) {
          const expected = total / count;
          if (Math.abs(expected - num(item)) > Math.max(0.5, Math.abs(expected) * 0.01)) {
            add("average-not-its-own-total", here, `${pageName} shows an average that is not its own total divided by its own number of things, so the figure and the rows behind it are two different answers.`);
          }
        }
      }

      // A stated total beside the list it is the total of.
      const totalOf = /^(.*?)_?(total|count)$/i.exec(key);
      if (totalOf && num(item) !== null && totalOf[1]) {
        const stem = totalOf[1].replace(/^total_?/i, "");
        const listKey = Object.keys(value).find((k) => Array.isArray(value[k]) && (k === stem || k === `${stem}s` || k.replace(/s$/, "") === stem.replace(/s$/, "")));
        const truncatedKey = Object.keys(value).find((k) => TRUNCATED_WORDS.test(k) && k.toLowerCase().includes(stem.toLowerCase().slice(0, 4)));
        const truncated = truncatedKey ? value[truncatedKey] === true : false;
        const anyTruncation = Object.entries(value).some(([k, v]) => TRUNCATED_WORDS.test(k) && v === true);
        if (listKey && !truncated && !anyTruncation && value[listKey].length !== num(item)) {
          add("list-disagrees-with-its-total", here, `${pageName} says one number of things and then lists a different number of them, with nothing on the screen to explain the difference.`);
        }
      }

      walk(item, here);
    }
  };

  walk(payload, "");
  return findings;
}
