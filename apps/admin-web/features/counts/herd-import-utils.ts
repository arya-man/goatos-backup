export async function stableCSVContentHash(input: string): Promise<string> {
  const bytes = new TextEncoder().encode(input);
  const digest = await globalThis.crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

export const sheetImportAccept = ".csv,text/csv,.xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet";

export function isSpreadsheetFile(name: string, type = ""): boolean {
  const normalizedName = name.toLowerCase();
  return normalizedName.endsWith(".xlsx") || type === "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet";
}

export async function spreadsheetArrayBufferToCSV(
  buffer: ArrayBuffer,
  emptyWorkbookMessage: string,
  parseFailedMessage: string,
): Promise<string> {
  const { readSheet } = await import("read-excel-file/universal");
  let rows: unknown[][];
  try {
    rows = await readSheet(buffer, 1, { trim: false });
  } catch {
    throw new Error(parseFailedMessage);
  }
  const csv = rows
    .filter((row) => row.some((cell) => spreadsheetCellText(cell).trim() !== ""))
    .map((row) => row.map((cell) => csvCell(spreadsheetCellText(cell))).join(","))
    .join("\n");
  if (!csv.trim()) {
    throw new Error(emptyWorkbookMessage);
  }
  return csv;
}

function spreadsheetCellText(value: unknown): string {
  if (value == null) return "";
  if (value instanceof Date) return value.toISOString();
  return String(value);
}

function neutralizeSpreadsheetFormula(text: string): string {
  if (text.startsWith("'")) return text;
  if (/^[\t\r\n]/.test(text) || /^\s*[=+\-@]/.test(text)) return `'${text}`;
  return text;
}

export function csvCell(value: unknown): string {
  const text = neutralizeSpreadsheetFormula(String(value ?? ""));
  return /[",\r\n]/.test(text) ? `"${text.replaceAll("\"", "\"\"")}"` : text;
}

export function parseCSVRecords(raw: string, options: { trimCells?: boolean; unterminatedQuoteMessage?: string } = {}): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = "";
  let inQuotes = false;
  const finishCell = () => {
    row.push(options.trimCells ? cell.trim() : cell);
    cell = "";
  };
  for (let i = 0; i < raw.length; i += 1) {
    const ch = raw[i];
    if (ch === "\"") {
      if (inQuotes && raw[i + 1] === "\"") {
        cell += "\"";
        i += 1;
      } else {
        inQuotes = !inQuotes;
      }
      continue;
    }
    if (ch === "," && !inQuotes) {
      finishCell();
      continue;
    }
    if ((ch === "\n" || ch === "\r") && !inQuotes) {
      if (ch === "\r" && raw[i + 1] === "\n") i += 1;
      finishCell();
      rows.push(row);
      row = [];
      continue;
    }
    cell += ch;
  }
  if (inQuotes) {
    throw new Error(options.unterminatedQuoteMessage ?? "CSV contains an unterminated quoted cell.");
  }
  if (cell.length > 0 || row.length > 0) {
    finishCell();
    rows.push(row);
  }
  return rows;
}
