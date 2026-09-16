package postgres

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// PARK ORDER IS THE SQL'S (park CODE: CBE, then CPT), and the decoder's re-sort must keep it. It
// used to compare the full park name, under which "Channapatna" (CPT) leads "Coimbatore" (CBE) --
// so every elevated/ground members hint opened on CPT (maintainer decision 2026-09-16).
func TestDecodeShedTypeMembersKeepsTheServedParkOrderNotTheNameOrder(t *testing.T) {
	// Rows arrive as the SQL orders them: label, shed type, park code -- Coimbatore first.
	raw := []byte(`[
	  ["Beetal","elevated","aaaaaaaa-0000-4000-8000-000000000001",null,"Godel 2","p-cbe","Coimbatore"],
	  ["Beetal","elevated","aaaaaaaa-0000-4000-8000-000000000002",null,"Yashoda","p-cbe","Coimbatore"],
	  ["Beetal","elevated","aaaaaaaa-0000-4000-8000-000000000003",null,"Castro","p-cpt","Channapatna"],
	  ["Beetal","ground","aaaaaaaa-0000-4000-8000-000000000004",null,"Castro","p-cbe","Coimbatore"],
	  ["Beetal","ground","aaaaaaaa-0000-4000-8000-000000000005",null,"Gandhi","p-cpt","Channapatna"]
	]`)
	got, err := decodeShedTypeMembers(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []string{"Coimbatore", "Coimbatore", "Channapatna", "Coimbatore", "Channapatna"}
	if len(got) != len(want) {
		t.Fatalf("got %d members, want %d: %+v", len(got), len(want), got)
	}
	for i, member := range got {
		if member.ParkName != want[i] {
			t.Fatalf("member %d is %s (%s), want %s: full order %v", i, member.ParkName, member.OperationalLocationDisplay, want[i], parkNames(got))
		}
	}
}

func parkNames(members []domain.ShedTypeMember) []string {
	out := make([]string, 0, len(members))
	for _, m := range members {
		out = append(out, m.ParkName+"/"+m.OperationalLocationDisplay)
	}
	return out
}

// SQL orders label, shed type, then park code. A sparse earlier group must
// never determine the park order of a later group; pen names still sort naturally.
func TestDecodeShedTypeMembersParkOrderIsScopedToEachGroup(t *testing.T) {
	for _, tc := range []struct{ name, firstLabel, firstType string }{
		{"different shed type", "Beetal", "elevated"},
		{"different breed", "Barbari", "ground"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal([][]any{
				{tc.firstLabel, tc.firstType, "shed1", nil, "Castro", "cpt", "Channapatna"},
				{"Beetal", "ground", "shed2", nil, "Yashoda 10", "cbe", "Coimbatore"},
				{"Beetal", "ground", "shed3", nil, "Yashoda 2", "cbe", "Coimbatore"},
				{"Beetal", "ground", "shed4", nil, "Gandhi", "cpt", "Channapatna"},
			})
			if err != nil {
				t.Fatal(err)
			}
			got, err := decodeShedTypeMembers(raw)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, member := range got {
				ids = append(ids, member.LocationID)
			}
			want := []string{"shed1", "shed3", "shed2", "shed4"}
			if !reflect.DeepEqual(ids, want) {
				t.Fatalf("member order = %v, want %v (CBE before CPT within each group, natural pen order)", ids, want)
			}
		})
	}
}
