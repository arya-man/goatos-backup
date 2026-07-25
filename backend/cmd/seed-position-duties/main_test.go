package main

import "testing"

func TestSeedFixtureVaccinationOwnerPositionsMapToVaccinationDuties(t *testing.T) {
	duties, st := deriveDuties([]positionRow{
		{positionCode: "shed_manager", positionTier: "manager"},
		{positionCode: "park_head", positionTier: "head"},
		{positionCode: "backup_manager", positionTier: "manager", isBackupSlot: true},
	})
	if st.UnmappedSkipped != 0 {
		t.Fatalf("unmapped=%d prefixes=%v, want all seed owner positions mapped", st.UnmappedSkipped, st.UnmappedPrefixes)
	}
	if len(duties) != 3 {
		t.Fatalf("duties=%d, want 3", len(duties))
	}
	for _, duty := range duties {
		if duty.moduleCode != "pc.vaccination" {
			t.Fatalf("%s module=%s, want pc.vaccination", duty.positionCode, duty.moduleCode)
		}
		if duty.dutyType != "execute" {
			t.Fatalf("%s duty=%s, want execute", duty.positionCode, duty.dutyType)
		}
		if duty.capability != vaccinationExecuteCapability {
			t.Fatalf("%s capability=%s, want %s", duty.positionCode, duty.capability, vaccinationExecuteCapability)
		}
	}
}
