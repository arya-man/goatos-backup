"use client";

import type { ComponentProps, ReactNode } from "react";
import { useRouter } from "next/navigation";

import { OrderTableToolbar } from "@/components/app/sections/order/order-table-toolbar";
import { pushLocalOverlayUrl } from "@/components/local-overlay-link";
import { Iconify } from "@/components/minimal/iconify";

export type RegisterMenuLink = { key: string; label: string; icon: ComponentProps<typeof Iconify>["icon"]; href: string };

/**
 * The register card's list toolbar (TR1-#28): the template OrderTableToolbar, whose ⋮ popover holds
 * the register's secondary actions (edit list, download/upload sheet, setup workbook) instead of
 * outlined buttons in the CardHeader. Each item opens its same-page drawer the way the old buttons
 * did (LocalOverlayLink semantics: a local history entry, no RSC request); an href on another path
 * soft-navigates. Server pages pass hrefs, never functions.
 */
export function RegisterToolbar({ filters, search, menuLinks, menuLabel }: { filters?: ReactNode; search?: ReactNode; menuLinks: RegisterMenuLink[]; menuLabel: string }) {
  const router = useRouter();
  return (
    <OrderTableToolbar
      filters={filters}
      search={search}
      menuLabel={menuLabel}
      menuActions={menuLinks.map((link) => ({
        key: link.key,
        label: link.label,
        icon: <Iconify icon={link.icon} />,
        onClick: () => {
          if (!pushLocalOverlayUrl(link.href)) router.push(link.href, { scroll: false });
        },
      }))}
    />
  );
}
