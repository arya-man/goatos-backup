import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// The shell is the MUI Minimal DashboardLayout (layouts/dashboard). These checks read the components
// that actually render the phone menu; the old `.side` / `.navscrim` CSS is not rendered any more.
const navMobile = readFileSync(new URL("../layouts/dashboard/nav-mobile.tsx", import.meta.url), "utf8");
const layout = readFileSync(new URL("../layouts/dashboard/layout.tsx", import.meta.url), "utf8");
const shell = readFileSync(new URL("./mesha-shell.tsx", import.meta.url), "utf8");

// Invariant bc8864617: while the phone menu is open no page content shows.
assert.match(
  navMobile,
  /backdrop:\s*\{\s*sx:\s*\{[^}]*bgcolor:\s*'var\(--bg\)'[^}]*backdropFilter:\s*'none'[^}]*\}/,
  "phone menu scrim must be the opaque page colour with no blur, so the page is hidden",
);
assert.match(
  navMobile,
  /'@media \(max-width: 860px\)':\s*\{[^}]*width:\s*'100vw'[^}]*\}/,
  "phone menu must be an opaque full-width menu at <=860px, not a narrow overlay over page content",
);
assert.match(navMobile, /bgcolor:\s*'var\(--layout-nav-bg\)'/, "phone menu paper must paint the Mesha sidebar colour");

// A visible close control stays in the phone menu.
assert.match(layout, /<IconButton onClick=\{onClose\} aria-label=\{closeLabel\} data-nav-close>/, "phone menu must keep a visible close button");
assert.match(shell, /closeLabel=\{shellCopy\(contract, "nav\.collapse"\)\}/, "phone menu close label comes from the shell copy contract");

// The page scrolls the body (template MainSection); `.main` must not become a scroll container again.
const glue = readFileSync(new URL("../layouts/mesha-layout.css", import.meta.url), "utf8");
assert.match(glue, /\.msh-content\.main\{[^}]*overflow:visible[^}]*\}/, "page content must not be its own scroll container");
assert.match(shell, /<DashboardContent[^>]*className="main msh-content"/, "page content keeps the `.main` class contract");

// Invariant bd0c2c286: a mini-rail group icon navigates to the group's first leaf (its leaves are hidden).
const navVertical = readFileSync(new URL("../layouts/dashboard/nav-vertical.tsx", import.meta.url), "utf8");
assert.match(navVertical, /<NavSectionMini[\s\S]*?enabledRootRedirect[\s\S]*?\/>/, "mini rail group icons must link to the group's first leaf");
assert.match(shell, /path: groupFirstHref\(g\)/, "group path is the first enabled leaf");
