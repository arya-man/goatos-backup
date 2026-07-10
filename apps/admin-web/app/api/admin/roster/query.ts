import { type NextRequest } from "next/server";

export function stringParam(request: NextRequest, name: string): string | undefined {
  const value = request.nextUrl.searchParams.get(name)?.trim();
  return value ? value : undefined;
}

export function positiveIntParam(request: NextRequest, name: string): number | undefined {
  const value = Number(request.nextUrl.searchParams.get(name));
  return Number.isInteger(value) && value > 0 ? value : undefined;
}

export function booleanParam(request: NextRequest, name: string): boolean | undefined {
  const value = request.nextUrl.searchParams.get(name);
  if (value === "true") return true;
  if (value === "false") return false;
  return undefined;
}
