import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readdirSync, readFileSync } from "node:fs";

// guard: ask-mesha-template-chat (J1B P1-2). Ask Mesha was a hand-built chat: ceo-ai-styles.tsx held
// 92 exported *Sx constants (the old stylesheet re-expressed as sx). It is now the template chat app
// (Minimal v7.7.0 sections/chat): ChatLayout verbatim under components/minimal/sections/chat and the
// template-derived ChatNav / ChatNavItem / ChatHeaderDetails / ChatMessageList / ChatMessageItem /
// ChatMessageInput under components/app/sections/chat (anatomy pinned by design:guard
// template-derived-anatomy). This test keeps it that way.
const here = new URL("./", import.meta.url);
const read = (p) => readFileSync(new URL(p, here), "utf8");
const panel = read("./ceo-ai-panel.tsx");
const derived = JSON.parse(read("../../../../docs/design/template-derived.json")).files;
const sources = JSON.parse(read("../../../../docs/design/template-sources.json")).sources;

test("the hand-made assistant stylesheet stays deleted", () => {
  assert.equal(existsSync(new URL("./ceo-ai-styles.tsx", here)), false);
  for (const f of readdirSync(here).filter((n) => /\.tsx?$/.test(n))) {
    assert.doesNotMatch(read(`./${f}`), /ceo-ai-styles/, `${f} imports the deleted stylesheet`);
  }
});

test("the panel composes the template chat section", () => {
  assert.match(panel, /from "@\/components\/minimal\/sections\/chat\/layout"/);
  for (const part of ["chat-nav", "chat-nav-item", "chat-header-details", "chat-message-list", "chat-message-item", "chat-message-input"]) {
    assert.match(panel, new RegExp(`from "@/components/app/sections/chat/${part}"`), `panel renders ${part}`);
    assert.ok(derived[`components/app/sections/chat/${part}.tsx`], `${part} is a declared template-derived file`);
  }
  assert.ok(sources["components/minimal/sections/chat/layout.tsx"], "ChatLayout is a verbatim template file");
  assert.ok(sources["components/minimal/markdown/styles.tsx"], "answers use the template MarkdownRoot");
});

test("no hand-rolled chat anatomy or sx stylesheet in the assistant", () => {
  assert.doesNotMatch(panel, /\bButtonBase\b|\bInputBase\b/, "controls are template IconButton / Chip / the derived input");
  for (const f of readdirSync(here).filter((n) => /\.tsx?$/.test(n))) {
    const n = (read(`./${f}`).match(/export const \w+Sx\b/g) ?? []).length;
    assert.ok(n === 0, `${f} exports ${n} *Sx constants (sx-as-stylesheet); compose template parts instead`);
    const consts = (read(`./${f}`).match(/^const [A-Z_]+_SX\b/gm) ?? []).length;
    assert.ok(consts <= 3, `${f} declares ${consts} local sx blocks; the look comes from the template chat section`);
  }
});

test("behaviour hooks survive the rebuild", () => {
  assert.match(panel, /readCeoAiStream\(/, "streaming answers");
  assert.match(panel, /aria-label=\{CHROME\.stop\}/, "the stop button");
  assert.match(panel, /<CopyButton text=\{message\.text\}/, "copy an answer");
  assert.match(panel, /<CeoAiChart chart=\{message\.chart\}/, "answer charts");
  assert.match(panel, /<Alert severity=/, "errors show as an Alert");
  assert.match(panel, /breakpoints\.down\("sm"\)\]: \{ inset: 0 \}/, "phone sheet fills the screen");
});
