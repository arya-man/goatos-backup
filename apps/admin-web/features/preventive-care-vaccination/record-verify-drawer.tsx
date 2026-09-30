"use client";

import { DrawerMetaGrid, DrawerMetaItem, DrawerNote } from "@/components/app/detail-drawer";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

// Read-only vaccination shed completion summary for the pen execution drawer: the record reason note
// over the template drawer meta grid (pen, vaccine). No fillable manual form fields.
export function VaccinationRecordFormFields({ cohortShed, vaccineName, pageContract }: { cohortShed: string; vaccineName: string; pageContract: AdminUiPageContract }) {
  return (
    <>
      <DrawerNote>{copy(pageContract, "drawer.record_verify.record_reason")}</DrawerNote>
      <DrawerMetaGrid>
        <DrawerMetaItem label={copy(pageContract, "drawer.record_verify.form.shed_name")}>{cohortShed}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "drawer.record_verify.form.vaccine")}>{vaccineName}</DrawerMetaItem>
      </DrawerMetaGrid>
    </>
  );
}
