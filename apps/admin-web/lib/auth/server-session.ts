import "server-only";

import { cookies } from "next/headers";
import { FIREBASE_ID_TOKEN_COOKIE } from "@/lib/auth/session-cookie";

export async function getFirebaseIdTokenCookie(): Promise<string | null> {
  const cookieStore = await cookies();
  const value = cookieStore.get(FIREBASE_ID_TOKEN_COOKIE)?.value.trim();
  return value || null;
}
