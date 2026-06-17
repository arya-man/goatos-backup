import { NextResponse } from "next/server";
import {
  DASHBOARD_BASE_PATH,
  FIREBASE_ID_TOKEN_COOKIE,
  maxAgeForFirebaseIdToken,
} from "@/lib/auth/session-cookie";

export const dynamic = "force-dynamic";

export async function POST(request: Request) {
  let body: unknown;
  try {
    body = await request.json();
  } catch {
    return NextResponse.json({ error: "invalid_json" }, { status: 400 });
  }

  const idToken = typeof (body as { idToken?: unknown }).idToken === "string" ? (body as { idToken: string }).idToken.trim() : "";
  const maxAge = maxAgeForFirebaseIdToken(idToken);
  if (!idToken || maxAge === null) {
    return NextResponse.json({ error: "invalid_or_expired_id_token" }, { status: 401 });
  }

  const response = NextResponse.json({ ok: true, maxAge });
  response.cookies.set({
    name: FIREBASE_ID_TOKEN_COOKIE,
    value: idToken,
    httpOnly: true,
    secure: true,
    sameSite: "lax",
    path: DASHBOARD_BASE_PATH,
    maxAge,
  });
  return response;
}

export async function DELETE() {
  const response = NextResponse.json({ ok: true });
  response.cookies.set({
    name: FIREBASE_ID_TOKEN_COOKIE,
    value: "",
    httpOnly: true,
    secure: true,
    sameSite: "lax",
    path: DASHBOARD_BASE_PATH,
    maxAge: 0,
  });
  return response;
}
