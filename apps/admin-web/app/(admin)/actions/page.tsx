import { redirect } from "next/navigation";

// Compatibility only. This screen was renamed Actions -> Verify (maintainer decision 2026-08-12):
// "Verify" is what the phone has always called it, and "Actions" said nothing about a screen that
// does exactly one thing. Kept so bookmarks, the nav leaves that deep-link with ?category=, and any
// link minted before the rename keep working.
export default function Page() {
  redirect("/verify");
}
