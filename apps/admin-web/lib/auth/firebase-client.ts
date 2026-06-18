"use client";

import { getApp, getApps, initializeApp, type FirebaseOptions } from "firebase/app";
import {
  GoogleAuthProvider,
  browserLocalPersistence,
  getAuth,
  setPersistence,
  signInWithPopup,
  signOut,
  type Auth,
  type User,
} from "firebase/auth";
import { FIREBASE_CONFIG_ROUTE, SESSION_ROUTE } from "@/lib/auth/session-cookie";

export type FirebaseClientRuntimeConfig = {
  config: FirebaseOptions;
};

export type FirebaseSessionEventType = "auth.sign_in" | "auth.session_refresh";

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

export async function signInWithGoogleAccountChooser(): Promise<User> {
  const auth = await getFirebaseAuth();
  const provider = new GoogleAuthProvider();
  provider.setCustomParameters({
    hd: "mesha.sg",
    prompt: "select_account",
  });
  provider.addScope("email");
  provider.addScope("profile");

  const result = await signInWithPopup(auth, provider);
  await syncFirebaseSession(result.user, true, "auth.sign_in");
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
    throw new Error("The admin session could not be refreshed.");
  }
  return true;
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
      return payload;
    });
  }
  return configPromise;
}
