import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: feed-config-plain-values (TR2-P2-2). Ration grams read as green primary links although
// they are plain figures; the compare value field's label ("Grams per head per day to compare
// against") was cut to "Grams per head per day to co…" in its 200px field.
const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");
const service = readFileSync(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");

test("guard: feed-config-plain-values - grams figures are text.primary, not primary.main", () => {
  for (const file of ["./feed-rate-optimistic.tsx", "./feed-config.tsx"]) {
    assert.doesNotMatch(read(file), /authoredZero \? "text\.secondary" : "primary\.main"/, file);
  }
});

test("guard: feed-config-plain-values - the compare value labels fit their 200px field", () => {
  for (const key of ["filter.grams_value_aria", "filter.kg_value_aria"]) {
    const m = service.match(new RegExp(`"${key.replace(".", "\\.")}":\\s*"([^"]+)"`));
    assert.ok(m, key);
    assert.ok(m[1].length <= 24, `${key} = "${m[1]}" is cut at 200px`);
  }
});
