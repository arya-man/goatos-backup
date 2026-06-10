import type { Metadata } from "next";
import { Inter } from "next/font/google";
import "./globals.css";
import { QueryProvider } from "@/lib/query-provider";

const inter = Inter({ subsets: ["latin"], variable: "--font-sans" });

export const metadata: Metadata = {
  title: "Goat OS Admin",
  description: "Goat OS Phase 1 admin readiness shell",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" className={`dark ${inter.variable}`}>
      <body className="font-sans antialiased bg-[#0F1115] text-[#FFFFFF]">
        <QueryProvider>{children}</QueryProvider>
      </body>
    </html>
  );
}
