import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const pickerUrl = new URL("../../components/top-bar-date-picker.tsx", import.meta.url);

test("top-bar date text is deterministic across server and browser locales", async () => {
  const source = await readFile(pickerUrl, "utf8");

  const date = new Date(2026, 6, 30);
  const options = { day: "2-digit", month: "short", year: "numeric" };
  const serverText = new Intl.DateTimeFormat("en-US", options).format(date);
  const browserText = new Intl.DateTimeFormat("en-GB", options).format(date);

  assert.equal(serverText, "Jul 30, 2026");
  assert.equal(browserText, "30 Jul 2026");
  assert.notEqual(serverText, browserText, "the reported SSR/client locale mismatch must remain reproduced");

  assert.doesNotMatch(
    source,
    /new Intl\.DateTimeFormat\(undefined,/,
    "ambient locale formatting makes the server and hydrated client render different text",
  );
  assert.match(source, /const DATE_DISPLAY_LOCALE = "en-GB";/);
  assert.equal(
    source.match(/new Intl\.DateTimeFormat\(DATE_DISPLAY_LOCALE,/g)?.length,
    4,
    "every date label rendered by the picker must use the same explicit locale",
  );
});
