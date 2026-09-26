// Ravi R2-4 guard: right drawers must be the template temporary Drawer (backdrop, template width).
import test from "node:test";
import assert from "node:assert/strict";
import { drawerTagLines, drawerTemplateFindings, onlyTemplateDrawerWidths } from "./drawer-template.mjs";

const lines = (src, rel = "features/x.tsx") => drawerTemplateFindings(src, rel).map((f) => f.line);

test("template MinimalDrawer / DetailDrawer at template widths pass", () => {
  assert.deepEqual(lines('<MinimalDrawer open onClose={c} title="t" width={480}>\n<div />\n</MinimalDrawer>'), []);
  assert.deepEqual(lines('<MinimalDrawer open onClose={c} title="t" width={320} />'), []);
  assert.deepEqual(lines('<DetailDrawer open onClose={c} title="t" closeLabel="Close" size="md" />'), []);
  assert.deepEqual(lines('const DRAWER_WIDTH = 360;\n<MinimalDrawer open onClose={c} title="t" width={DRAWER_WIDTH} />'), []);
});

test("off-template widths, transparent backdrops and raw MUI drawers fail", () => {
  assert.deepEqual(lines('const DRAWER_WIDTH = 380;\n<MinimalDrawer open onClose={c} title="t" width={DRAWER_WIDTH} />'), [2]);
  assert.deepEqual(lines('<MinimalDrawer open onClose={c} title="t" width={640} />'), [1]);
  assert.deepEqual(lines('<DetailDrawer open onClose={c} title="t" closeLabel="x" size="xl" />'), [1]);
  assert.deepEqual(lines('<MinimalDrawer open onClose={c} title="t" invisibleBackdrop />'), [1]);
  assert.deepEqual(lines('<Drawer anchor="right" open={o} onClose={c} slotProps={{ backdrop: { invisible: true } }} />'), [1]);
  assert.deepEqual(lines('<Drawer open={o} onClose={c} />'), [1]);
});

test("a left nav drawer and the template shell itself are not right drawers", () => {
  assert.deepEqual(lines('<Drawer anchor="left" open={o} onClose={c} />'), []);
  assert.deepEqual(lines('<Drawer anchor="right" open={o} onClose={c} />', "components/minimal/drawer/minimal-drawer.tsx"), []);
});

test("legacy hand-rolled drawers fail, including multi-line <aside> tags", () => {
  assert.deepEqual(lines('<aside className={`drawer${open ? " on" : ""}`} aria-label="x">'), [1]);
  assert.deepEqual(lines('<div>\n<aside\n  className="drawer on sales-tagdrawer"\n  aria-label="x"\n>'), [2]);
  assert.deepEqual(lines('<aside className="schedule-side-drawer" role="dialog">'), [1]);
  assert.deepEqual(lines('<aside className="lt-side">'), []);
});

test("fixed-px-width exemption only covers template widths inside a drawer tag", () => {
  const src = '<MinimalDrawer\n  open\n  width={480}\n>\n</MinimalDrawer>\n<div style={{ width: 480 }} />';
  const inTag = drawerTagLines(src);
  assert.ok(inTag.has(3));
  assert.ok(!inTag.has(6));
  assert.ok(onlyTemplateDrawerWidths("  width={480}"));
  assert.ok(!onlyTemplateDrawerWidths("  width={640}"));
});

test("a comment with an apostrophe inside a drawer prop does not swallow the body", () => {
  const src = '<DetailDrawer\n  open\n  footer={<>{/* the sale\'s count */}<Button /></>}\n>\n<Table size="small" />\n</DetailDrawer>';
  assert.deepEqual(lines(src), []);
});
