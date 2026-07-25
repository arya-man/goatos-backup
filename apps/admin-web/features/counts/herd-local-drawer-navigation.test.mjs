import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pageSource = readFileSync(new URL("./herd-register.tsx", import.meta.url), "utf8");
const drawerSource = readFileSync(new URL("./herd-passport-local-drawer.tsx", import.meta.url), "utf8");
const contractSource = readFileSync(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");

test("Herd passport previews open and close locally without route navigation", () => {
  assert.match(pageSource, /LocalOverlayLink/);
  assert.match(pageSource, /HerdPassportLocalDrawer/);
  assert.match(drawerSource, /useLocalOverlaySelection/);
  assert.doesNotMatch(drawerSource, /<Link[^>]+className="veil"/);
  assert.match(drawerSource, /href=\{`\/goats\/\$\{encodeURIComponent\(item\.goatId\)\}`\}/);
  assert.match(drawerSource, /<HerdReproductiveEdit/);
});

test("Herd passport drawer includes goat-wise vaccination passport history", () => {
  assert.match(drawerSource, /\/api\/goats\/\$\{encodeURIComponent\(goatId\)\}\/vaccination-passport/);
  assert.match(drawerSource, /vaccination_history/);
  assert.match(drawerSource, /open_obligations/);
  assert.match(drawerSource, /HerdDrawerVaccinationBlock/);
});

test("Herd passport vaccination fetch fails visibly instead of spinning forever", () => {
  assert.match(drawerSource, /try \{/);
  assert.match(drawerSource, /response\.json\(\)\.catch\(\(\) => \(\{\}\)\)/);
  assert.match(drawerSource, /vaccination_passport_unreachable/);
  assert.match(drawerSource, /setVaccinationErrors/);
});

test("shared Herd passport drawer does not require the herd table contract on Calendar", () => {
  assert.match(drawerSource, /function passportIdentityLabels/);
  assert.match(drawerSource, /pageContract\.route_id === "herd-register"/);
  assert.match(drawerSource, /calendar\.drive\.display_id_header/);
  assert.match(drawerSource, /tableLabels\(pageContract, "herd-register"\)/);
  assert.doesNotMatch(drawerSource, /PASSPORT_COPY_FALLBACKS|PASSPORT_IDENTITY_LABELS|drawerTableLabels|function passportCopy/);
  assert.match(contractSource, /case "calendar":[\s\S]*"drawer\.passport\.aria"/);
  assert.match(contractSource, /page\("calendar"[\s\S]*table\("vaccination-open-obligations"/);
  assert.match(contractSource, /page\("calendar"[\s\S]*table\("vaccination-history"/);
  assert.match(drawerSource, /const canEditReproductiveStatus = pageContract\.route_id === "herd-register"/);
});
