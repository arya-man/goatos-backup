import { NextResponse } from "next/server";
import type { FirebaseOptions } from "firebase/app";

export const dynamic = "force-dynamic";

export function GET() {
  const config = firebaseConfigFromEnv();
  if (!config) {
    return NextResponse.json({ error: "firebase_config_missing" }, { status: 503 });
  }
  return NextResponse.json({ config }, { headers: { "Cache-Control": "no-store" } });
}

function firebaseConfigFromEnv(): FirebaseOptions | null {
  const raw = process.env.GOATOS_FIREBASE_WEB_CONFIG?.trim();
  if (raw) {
    try {
      const parsed = JSON.parse(raw) as FirebaseOptions;
      return isCompleteFirebaseConfig(parsed) ? parsed : null;
    } catch {
      return null;
    }
  }
  const config: FirebaseOptions = {
    apiKey: process.env.NEXT_PUBLIC_FIREBASE_API_KEY,
    authDomain: process.env.NEXT_PUBLIC_FIREBASE_AUTH_DOMAIN,
    projectId: process.env.NEXT_PUBLIC_FIREBASE_PROJECT_ID,
    appId: process.env.NEXT_PUBLIC_FIREBASE_APP_ID,
    messagingSenderId: process.env.NEXT_PUBLIC_FIREBASE_MESSAGING_SENDER_ID,
    storageBucket: process.env.NEXT_PUBLIC_FIREBASE_STORAGE_BUCKET,
  };
  return isCompleteFirebaseConfig(config) ? config : null;
}

function isCompleteFirebaseConfig(config: FirebaseOptions): boolean {
  return Boolean(config.apiKey && config.authDomain && config.projectId && config.appId);
}
