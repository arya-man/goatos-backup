import { AppSidebar } from "@/components/layout/app-sidebar";
import { Navbar } from "@/components/layout/navbar";
import { FirebaseSessionBridge } from "@/components/auth/firebase-session-bridge";
import { SidebarProvider } from "@/components/layout/sidebar-context";

export function AdminShell({ children }: { children: React.ReactNode }) {
  return (
    <SidebarProvider>
      <div className="flex h-screen overflow-hidden bg-[#0f1115] text-[#f8fafc]">
        <AppSidebar />
        <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
          <Navbar />
          <FirebaseSessionBridge />
          <main className="min-h-0 flex-1 overflow-x-hidden overflow-y-auto p-3 sm:p-6">{children}</main>
        </div>
      </div>
    </SidebarProvider>
  );
}
