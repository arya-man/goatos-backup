import { AuthSplitLayout } from "@/layouts/auth-split";
import { PasswordResetAction } from "@/components/auth/password-reset-action";
import { type RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function FirebaseAuthActionPage({ searchParams }: { searchParams?: Promise<RouteSearchParams> }) {
  const sp = (await searchParams) ?? {};
  const mode = firstSearchParam(sp.mode) ?? "";
  const oobCode = firstSearchParam(sp.oobCode) ?? "";
  const continueHref = safeContinueHref(firstSearchParam(sp.continueUrl));

  return (
    <AuthSplitLayout
      logoText="M"
      slotProps={{
        section: {
          title: "Reset access for herd operations.",
          subtitle: "Protected recovery for approved Mesha dashboard accounts.",
        },
      }}
    >
      <PasswordResetAction mode={mode} oobCode={oobCode} continueHref={continueHref} />
    </AuthSplitLayout>
  );
}

function firstSearchParam(value: RouteSearchParams[string]): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}

function safeContinueHref(value: string | undefined): string {
  if (!value) return "/login";
  try {
    const url = new URL(value);
    if (isAllowedDashboardHost(url.hostname)) {
      return `${url.pathname}${url.search}${url.hash}` || "/login";
    }
  } catch {
    if (value.startsWith("/") && !value.startsWith("//") && !value.includes("\\")) {
      return value;
    }
  }
  return "/login";
}

function isAllowedDashboardHost(hostname: string): boolean {
  return (
    hostname === "dashboard.mesha.sg" ||
    hostname === "localhost" ||
    hostname === "127.0.0.1"
  );
}
