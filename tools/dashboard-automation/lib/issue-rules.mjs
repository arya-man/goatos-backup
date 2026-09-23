// The rule table that turns a raw check failure into the headline a person reads in Slack.
// Extracted from notify-slack.mjs so it can be unit-tested: importing that file runs its CLI.
//
// Each entry matches EITHER the plain sentence the smoke now throws OR the older coded form,
// so receipts written before the wording change still classify.
export function staticIssueRules() {
  return [
  [/A-svg-text-tiny|renders at ~|far too small to read/, "Chart text too small to read"],
  [/A-chart-label-column-narrow|A-chart-label-ellipsised|A-chart-label-collapsed|A-chart-label-clipped|A-chart-label-overlap|A-svg-text-(overlap|clipped)|A-chart-value-missing|A-chart-empty|squeezed so narrow|cut off at the edge of its card|squashed until there is no room|printed on top of each other|labels in this chart are cut short|no number beside it|is an empty frame|runs outside the chart|chart labels .* collide/, "Chart labels squashed, cut off or missing"],
  [/text-overlap|overlaps |is printed on top of/, "Text drawn on top of other text"],
  [/chip-crushed|crushed out of shape/, "Label crushed / cut off"],
  [/C-cell-mid-word-wrap|split over \d+ lines|is broken across two lines/, "Word broken across two lines"],
  [/C-cell-overpaint|spills over the next column/, "Table text spilling into the next column"],
  [/ISO date|written year-first/, "Date shown as YYYY-MM-DD (farm reads DD/MM/YYYY)"],
  [/snake_case code|copy key|raw value "NaN|raw value|shows an internal code/, "Internal code shown to users"],
  [/doubled label|printed twice over/, "Label repeated twice"],
  [/B-container-overflow|past \.card|cut at the viewport edge|panels cut|runs outside the card/, "Content spilling out of its card"],
  [/D-page-overflow|horizontal overflow|scrolls sideways/, "Page scrolls sideways on the phone"],
  [/cannot be horizontally scrolled/, "Wide table cut off with no sideways scroll"],
  [/interactive targets below 40px/, tapTargetLabel],
  [/clipped button\/link text/, "Button text cut off"],
  [/text-cut-off|text hidden|is cut off, with no/, "Text cut off"],
  [/overlay .*did not open|never mounted/, "Clicking it did not open"],
  [/feature missing/, "Feature missing or broken"],
  [/header not visible at the top/, "Drawer opens with its title bar scrolled out of view"],
  [/overlapping interactive elements/, "Buttons overlapping each other"],
  [/anchored to ancestor|backdrop-f/, "Popup opens in the wrong place (pinned to the header, not the screen)"],
  [/locator\.click: Timeout/, "A button on the page could not be clicked"],
  [/Smoke route redirected/, null],
  [/overlay .*off-screen|outside the viewport|translate/, "Drawer/popup opens off-screen"],
  [/page load \d+ms exceeded/, "Page slow to load"],
  [/accessibility violations/, null],
  [/new commit\(s\) need smoke coverage/, "New work shipped with no smoke check covering it"],
  [/assertion\(s\) need review/, "Some smoke checks point at screen text that no longer exists"],
  // Pen / partition labels. The page name and the pen text itself come from the shared
  // formatting below, so these read as e.g.
  //   Weights Analytics (phone) — A pen is shown with its part number twice — "Godel 1 - Part 1 - Part 1"
  [/P-pen-part-doubled|shown with its part number twice/, "A pen is shown with its part number twice"],
  [/P-pen-number-doubled|shown with its number twice/, "A pen is shown with its number twice"],
  [/P-pen-partition-missing|shown without its part number/, "A pen is shown without its part number"],
  [/P-pen-separator-wrong|joined in the wrong style/, "A pen's name is written in the wrong style"],
  [/P-pen-whole-leaked|the word whole instead of the shed name/, "A pen shows the word whole instead of the shed name"],
  ];
}

// A label may be a function when one check covers several kinds of element and the sentence has
// to name the one that was measured. "Buttons too small to tap" was posted about an <input>: the
// tasks search box is a box you type in, not a button, and the same check also covers links.
// Read the element the check dumped, and fall back to wording that is true for all of them.
export function tapTargetLabel(raw) {
  const kinds = new Set([...String(raw).matchAll(/"tag":"([a-z]+)"/g)]
    .map(([, tag]) => (["input", "textarea", "select"].includes(tag) ? "field" : tag === "a" ? "link" : "button")));
  if (kinds.size !== 1) return "Too small to tap on a phone";
  return { field: "Box you type in is too small to tap", link: "Links too small to tap", button: "Buttons too small to tap" }[[...kinds][0]];
}
