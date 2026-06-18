"use client";

import { getApp, getApps, initializeApp, type FirebaseOptions } from "firebase/app";
import {
  GoogleAuthProvider,
  browserLocalPersistence,
  getAuth,
  setPersistence,
  signInWithCredential,
  signOut,
  type Auth,
  type User,
} from "firebase/auth";
import { FIREBASE_CONFIG_ROUTE, SESSION_ROUTE } from "@/lib/auth/session-cookie";

export type FirebaseClientRuntimeConfig = {
  config: FirebaseOptions;
  googleClientId: string;
};

export type FirebaseSessionEventType = "auth.sign_in" | "auth.session_refresh";

export class FirebaseSessionError extends Error {
  constructor(
    message: string,
    readonly code: string,
  ) {
    super(message);
    this.name = "FirebaseSessionError";
  }
}

let authPromise: Promise<Auth> | null = null;
let configPromise: Promise<FirebaseClientRuntimeConfig> | null = null;

export async function getFirebaseAuth(): Promise<Auth> {
  if (!authPromise) {
    authPromise = loadFirebaseConfig().then(async ({ config }) => {
      const app = getApps().some((candidate) => candidate.name === "goatos-admin-web")
        ? getApp("goatos-admin-web")
        : initializeApp(config, "goatos-admin-web");
      const auth = getAuth(app);
      await setPersistence(auth, browserLocalPersistence);
      return auth;
    });
  }
  return authPromise;
}

export async function getFirebaseClientRuntimeConfig(): Promise<FirebaseClientRuntimeConfig> {
  return loadFirebaseConfig();
}

export async function signInWithGoogleIdToken(googleIdToken: string): Promise<User> {
  const auth = await getFirebaseAuth();
  const credential = GoogleAuthProvider.credential(googleIdToken);
  const result = await signInWithCredential(auth, credential);
  try {
    await syncFirebaseSession(result.user, true, "auth.sign_in");
  } catch (error) {
    await signOut(auth).catch(() => undefined);
    throw error;
  }
  return result.user;
}

export async function clearFirebaseSession(): Promise<void> {
  const auth = await getFirebaseAuth();
  await fetch(SESSION_ROUTE, { method: "DELETE", cache: "no-store" });
  await signOut(auth);
}

export async function syncFirebaseSession(
  user: User | null,
  forceRefresh = false,
  eventType: FirebaseSessionEventType = "auth.session_refresh",
): Promise<boolean> {
  if (!user) {
    await fetch(SESSION_ROUTE, { method: "DELETE", cache: "no-store" });
    return false;
  }
  const idToken = await user.getIdToken(forceRefresh);
  const response = await fetch(SESSION_ROUTE, {
    method: "POST",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ idToken, eventType }),
  });
  if (!response.ok) {
    const code = await sessionRouteErrorCode(response);
    throw new FirebaseSessionError(messageForSessionRouteError(code), code);
  }
  return true;
}

export function isFirebaseSessionError(error: unknown, code?: string): error is FirebaseSessionError {
  return (
    error instanceof FirebaseSessionError &&
    (code === undefined || error.code === code)
  );
}

async function loadFirebaseConfig(): Promise<FirebaseClientRuntimeConfig> {
  if (!configPromise) {
    configPromise = fetch(FIREBASE_CONFIG_ROUTE, { cache: "no-store" }).then(async (response) => {
      if (!response.ok) {
        throw new Error("Firebase sign-in is not configured for this admin deployment.");
      }
      const payload = (await response.json()) as FirebaseClientRuntimeConfig;
      if (!payload.config?.apiKey || !payload.config.authDomain || !payload.config.projectId || !payload.config.appId) {
        throw new Error("Firebase sign-in config is incomplete.");
      }
      if (!payload.googleClientId) {
        throw new Error("Google sign-in client ID is not configured for this admin deployment.");
      }
      return payload;
    });
  }
  return configPromise;
}

async function sessionRouteErrorCode(response: Response): Promise<string> {
  let payload: unknown;
  try {
    payload = await response.json();
  } catch {
    return "admin_session_failed";
  }
  if (isRecord(payload) && typeof payload.error === "string" && payload.error.trim() !== "") {
    return payload.error.trim();
  }
  return "admin_session_failed";
}

function messageForSessionRouteError(code: string): string {
  switch (code) {
    case "email_not_allowed":
      return "This Google account is not allowed for Mesha Admin.";
    case "tenant_not_allowed":
    case "tenant_config_missing":
      return "Mesha Admin sign-in is misconfigured for this environment.";
    case "invalid_bearer_token":
    case "invalid_or_expired_id_token":
      return "Google sign-in did not return a valid Mesha session. Try again.";
    default:
      return "The admin session could not be refreshed.";
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
