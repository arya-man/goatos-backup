"use client";

export async function stableCSVContentHash(input: string): Promise<string> {
  const bytes = new TextEncoder().encode(input);
  const digest = await globalThis.crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
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

