import { AuthRequiredPanel } from "@/components/admin-primitives";

export const dynamic = "force-dynamic";

export default function LoginPage() {
  return <AuthRequiredPanel />;
}
