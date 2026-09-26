// Display-only helpers for the Sales Config valuation section. Nothing here changes what is stored.
//
// Seeded stages (migration 000425) carry their raw code as the label ("K0".."K3"). The farm's own
// stage register already names those codes, so the screen shows that name instead -- and a save
// sends the stored label back unchanged unless the farm actually edits the name.

type RegisterEntry = { code: string; label: string };

function registerName(code: string, register: readonly RegisterEntry[]): string {
  const key = code.trim().toUpperCase();
  const hit = register.find((e) => e.code.trim().toUpperCase() === key);
  const name = hit?.label.trim() ?? "";
  return name && name.toUpperCase() !== key ? name : "";
}

/** The name to show for a stage: the stored label, or the register's name when the label is blank or just the code. */
export function displayStageLabel(stage: string, label: string, register: readonly RegisterEntry[]): string {
  const stored = label.trim();
  if (stage && (stored === "" || stored.toUpperCase() === stage.trim().toUpperCase())) {
    return registerName(stage, register) || label;
  }
  return label;
}

/** The label to save: the stored one when the farm left the displayed default untouched, else what was typed. */
export function storedStageLabel(stage: string, storedLabel: string, typed: string, register: readonly RegisterEntry[]): string {
  return typed === displayStageLabel(stage, storedLabel, register) ? storedLabel : typed;
}

/** "DD/MM/YYYY HH:MM" from the backend's "DD-MM-YYYY HH:MM" (IST); anything else is returned as is. */
export function fmtValuationSavedAt(raw: string): string {
  const m = /^(\d{2})-(\d{2})-(\d{4}) (\d{2}:\d{2})$/.exec(raw.trim());
  return m ? `${m[1]}/${m[2]}/${m[3]} ${m[4]}` : raw;
}
