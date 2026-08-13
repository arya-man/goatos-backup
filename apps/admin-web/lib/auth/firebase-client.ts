"use client";

import { getApp, getApps, initializeApp, type FirebaseOptions } from "firebase/app";
import {
  GoogleAuthProvider,
  browserLocalPersistence,
  confirmPasswordReset,
  getAuth,
  sendPasswordResetEmail,
  setPersistence,
  signInWithEmailAndPassword,
  signInWithCredential,
  signOut,
  verifyPasswordResetCode,
  type Auth,
  type ActionCodeSettings,
  type User,
} from "firebase/auth";
import { FIREBASE_CONFIG_ROUTE, LOGIN_PATH, SESSION_ROUTE } from "@/lib/auth/session-cookie";

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
  try {
    const result = await signInWithCredential(auth, credential);
    console.info("admin_firebase_login_succeeded", { method: "google", email: result.user.email, firebaseUid: result.user.uid });
    return syncSignedInUser(result.user);
  } catch (error) {
    console.warn("admin_firebase_login_failed", { method: "google", code: firebaseErrorCode(error) });
    throw error;
  }
}

export async function signInWithEmailPassword(email: string, password: string): Promise<User> {
  const auth = await getFirebaseAuth();
  try {
    const result = await signInWithEmailAndPassword(auth, email, password);
    console.info("admin_firebase_login_succeeded", { method: "password", email: result.user.email, firebaseUid: result.user.uid });
    return syncSignedInUser(result.user);
  } catch (error) {
    console.warn("admin_firebase_login_failed", { method: "password", email, code: firebaseErrorCode(error) });
    throw error;
  }
}

export async function sendPasswordReset(email: string): Promise<void> {
  const auth = await getFirebaseAuth();
  await sendPasswordResetEmail(auth, email, passwordResetActionCodeSettings());
}

export async function verifyPasswordReset(code: string): Promise<string> {
  const auth = await getFirebaseAuth();
  return verifyPasswordResetCode(auth, code);
}

export async function confirmPasswordResetCode(code: string, newPassword: string): Promise<void> {
  const auth = await getFirebaseAuth();
  await confirmPasswordReset(auth, code, newPassword);
}

async function syncSignedInUser(user: User): Promise<User> {
  const auth = await getFirebaseAuth();
  try {
    await syncFirebaseSession(user, true, "auth.sign_in");
  } catch (error) {
    console.warn("admin_firebase_session_sync_failed", { email: user.email, firebaseUid: user.uid, code: firebaseErrorCode(error) });
    await signOut(auth).catch(() => undefined);
    throw error;
  }
  return user;
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
    // refreshToken lets SSR mint fresh ID tokens after the id-token cookie's ~1h
    // TTL lapses, so the server session outlives a single ID token.
    body: JSON.stringify({ idToken, refreshToken: user.refreshToken, eventType }),
  });
  if (!response.ok) {
    const code = await sessionRouteErrorCode(response);
    throw new FirebaseSessionError(messageForSessionRouteError(code), code);
  }
  console.info("admin_firebase_session_sync_succeeded", { eventType, email: user.email, firebaseUid: user.uid });
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
    configPromise = fetch(FIREBASE_CONFIG_ROUTE, { cache: "no-store" })
      .then(async (response) => {
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
      })
      .catch((error: unknown) => {
        configPromise = null;
        throw error;
      });
  }
  return configPromise;
}

function passwordResetActionCodeSettings(): ActionCodeSettings {
  const origin = typeof window === "undefined" ? "https://stg.dashboard.mesha.sg" : window.location.origin;
  return {
    url: new URL(LOGIN_PATH, origin).toString(),
    handleCodeInApp: false,
  };
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
      return "This account is not allowed for Mesha Admin.";
    case "tenant_not_allowed":
    case "tenant_config_missing":
      return "Mesha Admin sign-in is misconfigured for this environment.";
    case "invalid_bearer_token":
    case "invalid_or_expired_id_token":
      return "Sign-in did not return a valid Mesha session. Try again.";
    default:
      return "The admin session could not be refreshed.";
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function firebaseErrorCode(error: unknown): string {
  if (isRecord(error) && typeof error.code === "string" && error.code.trim() !== "") {
    return error.code.trim();
  }
  if (error instanceof Error && error.name.trim() !== "") {
    return error.name;
  }
  return "unknown";
}
