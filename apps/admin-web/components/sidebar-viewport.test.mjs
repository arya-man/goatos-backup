import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// The shell is the MUI Minimal DashboardLayout (layouts/dashboard). These checks read the components
// that actually render the phone menu; the old `.side` / `.navscrim` CSS is not rendered any more.
const navMobile = readFileSync(new URL("../layouts/app/dashboard/nav-mobile.tsx", import.meta.url), "utf8");
const layout = readFileSync(new URL("../layouts/app/dashboard/layout.tsx", import.meta.url), "utf8");
const shell = readFileSync(new URL("./mesha-shell.tsx", import.meta.url), "utf8");

// R2 item 5 (Ravi 2026-09-26, supersedes invariant bc8864617): the phone menu is the template NavMobile
// drawer as shipped: var(--layout-nav-mobile-width) wide over the template backdrop, no full-width
// opaque scrim, no extra close button (backdrop tap, Escape and Android Back close it).
assert.doesNotMatch(navMobile, /100vw/, "phone menu keeps the template drawer width");
assert.doesNotMatch(navMobile, /backdrop:\s*\{\s*sx:/, "phone menu keeps the template backdrop");
assert.match(navMobile, /width:\s*'var\(--layout-nav-mobile-width\)'/, "phone menu paper is the template width");
assert.match(navMobile, /useBackCloses\(open, onClose\)/, "Android Back closes the phone menu");
assert.doesNotMatch(layout, /data-nav-close/, "no extra close button in the phone menu");

// No custom nav footer ("Mesha · goat operating system") and no default-open subtrees: the template
// nav ends with its items and opens only the active group.
for (const [name, text] of [["layout", layout], ["shell", shell], ["navMobile", navMobile]]) {
  assert.doesNotMatch(text, /navBottom|msh-foot|navigation\.footer/, `${name}: no custom nav footer`);
  assert.doesNotMatch(text, /defaultOpen|default_open/, `${name}: no default-open nav groups`);
}
const navList = readFileSync(new URL("../layouts/app/nav-section/vertical/nav-list.tsx", import.meta.url), "utf8");
assert.match(navList, /useBoolean\(isActive\)/, "nav groups open only when active (template rule)");

// Header right order follows the template: notifications, then the Settings slot (theme toggle), then account.
const bell = shell.indexOf("<NotificationBell");
const toggle = shell.indexOf("<ThemeToggle");
const account = shell.indexOf("<AccountButton");
assert.ok(bell > 0 && bell < toggle && toggle < account, "header order: notifications, theme toggle, account");
assert.match(shell, /<WorkspacesButton/, "park scope uses the template workspaces switcher trigger");

// The page scrolls the body (template MainSection); `.main` must not become a scroll container again.
const glue = readFileSync(new URL("../layouts/mesha-layout.css", import.meta.url), "utf8");
assert.match(glue, /\.msh-content\.main\{[^}]*overflow:visible[^}]*\}/, "page content must not be its own scroll container");
assert.match(shell, /<DashboardContent[^>]*className="main msh-content"/, "page content keeps the `.main` class contract");

// Invariant bd0c2c286: a mini-rail group icon navigates to the group's first leaf (its leaves are hidden).
const navVertical = readFileSync(new URL("../layouts/app/dashboard/nav-vertical.tsx", import.meta.url), "utf8");
assert.match(navVertical, /<NavSectionMini[\s\S]*?enabledRootRedirect[\s\S]*?\/>/, "mini rail group icons must link to the group's first leaf");
assert.match(shell, /path: groupFirstHref\(g\)/, "group path is the first enabled leaf");
