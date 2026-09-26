"use client";

// The Items section and the record-sale drawer share ONE list of what the farm sells.
//
// They are two components on one page, and the maintainer's requirement joins them: "i will add
// item ... while registering sales i need to see all this in dropdown". Holding the list here, in
// the one client boundary that contains both, is what lets an item added in the section appear in
// the drawer's dropdown without a page reload -- and without the save action revalidating the
// route, which would re-render the page on top of the row that was just applied.
import { useState } from "react";

import type { ProcurementVendorOptions } from "@/lib/api/server";
import type { AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { SalesDeal, SalesOptions, SellableProduct, SellableProductPage } from "@/lib/api/procurement";
import { SalesRecordDrawer } from "./sales-record-drawer";
import { SellableProductsSection } from "./sellable-products-section";

export function SalesItemsAndRecordDrawer({
  showProducts = true,
  productsPage,
  salesOptions,
  pageContract,
  canWriteProducts,
  productsDisabledReason,
  deals,
  listHref,
  canRecord,
  vendorOptions,
  stockConfirmNeeded,
  statusStockConfirmNeeded,
  stockConfirmDetail,
  children,
}: {
  /** The items card shows only on its own Sales Config tab; the list and the drawer stay mounted. */
  showProducts?: boolean;
  productsPage: SellableProductPage | null;
  salesOptions: SalesOptions | null;
  pageContract: AdminUiPageContract;
  canWriteProducts: boolean;
  productsDisabledReason: string;
  deals: SalesDeal[];
  listHref: string;
  canRecord: boolean;
  vendorOptions: ProcurementVendorOptions | null;
  stockConfirmNeeded: boolean;
  /** The same confirmation, raised by CLOSING an expected sale -- when its feed actually leaves. */
  statusStockConfirmNeeded: boolean;
  stockConfirmDetail?: string;
  /** The page's other sections, rendered between the two halves this component owns. */
  children?: React.ReactNode;
}) {
  const [products, setProducts] = useState<SellableProduct[] | null>(null);

  // The drawer offers the ACTIVE items, in the farm's order. Until something is edited this is the
  // server's own answer, untouched; afterwards it is the same list with the edit applied.
  const live = products ?? productsPage?.products ?? [];
  const options: SalesOptions | null = salesOptions
    ? {
        ...salesOptions,
        products: live
          .filter((p) => p.status === "active")
          .map((p) => ({ name: p.name, code: p.code, kind: p.kind, unit: p.unit, priced_per_unit: p.priced_per_unit })),
        // An item added since the page loaded has no variants read for it yet. `other` items sell
        // as themselves, which is the whole list they ever have; an animal or feed item added just
        // now shows its existing variants after the next load.
        breeds: Object.fromEntries(
          live
            .filter((p) => p.status === "active")
            .map((p) => [p.name, salesOptions.breeds?.[p.name] ?? (p.kind === "other" ? [p.name] : [])]),
        ),
      }
    : null;

  return (
    <>
      {showProducts ? (
        <SellableProductsSection
          page={productsPage}
          pageContract={pageContract}
          canWrite={canWriteProducts}
          disabledReason={productsDisabledReason}
          onProductsChanged={setProducts}
        />
      ) : null}
      {children}
      <SalesRecordDrawer
        deals={deals}
        pageContract={pageContract}
        listHref={listHref}
        canRecord={canRecord}
        vendorOptions={vendorOptions}
        salesOptions={options}
        stockConfirmNeeded={stockConfirmNeeded}
        statusStockConfirmNeeded={statusStockConfirmNeeded}
        stockConfirmDetail={stockConfirmDetail}
      />
    </>
  );
}
