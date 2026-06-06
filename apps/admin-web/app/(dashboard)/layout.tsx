import { AppSidebar } from "@/components/layout/app-sidebar";
import { Navbar } from "@/components/layout/navbar";
import { SidebarProvider } from "@/components/layout/sidebar-context";

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <SidebarProvider>
      <div className="flex h-screen overflow-hidden">
        <AppSidebar />
        <div className="flex flex-1 flex-col overflow-hidden">
          <Navbar />
          <main className="flex-1 overflow-y-auto p-3 sm:p-6">{children}</main>
        </div>
      </div>
    </SidebarProvider>
  );
}
