import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./inline-cell-editor.tsx", import.meta.url), "utf8");

test("portaled tag editor does not paint stale coordinates when reopened", () => {
  assert.match(source, /const \[popStyle, setPopStyle\] = useState<React\.CSSProperties \| null>\(null\)/);
  assert.match(source, /style=\{popStyle \?\? \{ position: "fixed", top: 0, left: 0, visibility: "hidden" \}\}/);
  assert.match(source, /function close\(\) \{[\s\S]*setPopStyle\(null\);[\s\S]*\}/);
});
