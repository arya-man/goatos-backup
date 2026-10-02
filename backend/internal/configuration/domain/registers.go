// Package domain is the rulebook of Configuration -> Items and settings (maintainer instruction
// 2026-09-18, from the Claude prototype merged in #297): the farm's reference lists -- places,
// animal types, catalogues -- as tenant-scoped REGISTERS a person edits on screen instead of a
// developer seeding them.
//
// The design centre is that a register is DATA, not a screen: one Register value names its
// columns, their types, which other register a column points at, and what a row's display name
// is. The HTTP contract serves those definitions and the admin-web page renders every register
// from them, so adding a list to the farm is one definition here plus one store in the postgres
// adapter -- never a new page. That is what "everything configurable, so a new owner can add
// their own animal types, places, feeds and medicines" needs to stay true as the product grows.
package domain

import "strings"

// Column types. The screen picks its input from the type; the validator its coercion.
const (
	TypeText   = "text"   // free text
	TypeCode   = "code"   // lower_snake identifier, immutable once written
	TypeNumber = "number" // integer or decimal, validated against Min when set
	TypeBool   = "bool"
	TypeEnum   = "enum" // one of Column.Options
	TypeRef    = "ref"  // the id of a row in Column.Ref
	TypeNotes  = "notes"
)

// Groups order the rail on the page.
const (
	GroupFarmPlaces  = "farm_places"
	GroupAnimalTypes = "animal_types"
	GroupCatalogue   = "catalogue"
	GroupPeople      = "people"
	GroupReference   = "reference_lists"
	GroupAnimals     = "animals"
)

// Option is one choice of an enum column.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Column is one field of a register.
type Column struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
	// Ref names the register whose rows this column points at (TypeRef).
	Ref string `json:"ref,omitempty"`
	// Options is the closed list of a TypeEnum column.
	Options []Option `json:"options,omitempty"`
	// Kinds restricts the column to items whose category kind is one of these; empty = every row.
	Kinds []string `json:"kinds,omitempty"`
	// ImpliedOutOfKind drops a value this column's kinds do not cover instead of refusing it,
	// because the register already knows the answer and the sheet merely repeated it. A feed
	// item's unit is always kg, so a filled-in template saying "kg" is agreeing, not asserting --
	// refusing it broke the onboarding import for every feed row. A column carrying a fact the
	// register would LOSE, such as a route on a vaccine, must NOT set this: there the refusal is
	// the point.
	ImpliedOutOfKind bool `json:"implied_out_of_kind,omitempty"`
	// Immutable columns are written once (a code) and refused on update.
	Immutable bool `json:"immutable,omitempty"`
	// Hint is a one-line help sentence under the input.
	Hint string `json:"hint,omitempty"`
	// Min bounds a number column.
	Min *float64 `json:"min,omitempty"`
	// Integer refuses a fractional number.
	Integer bool `json:"integer,omitempty"`
	// ListHidden keeps the column in the form but out of the table.
	ListHidden bool `json:"list_hidden,omitempty"`
	// Derived columns are composed by the store on read (a department, a tracking chip); they
	// are never in the form and a write naming one is dropped.
	Derived bool `json:"derived,omitempty"`
}

// Register is one editable list.
type Register struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// One is the singular noun for "Add <one>" and drawer titles.
	One     string   `json:"one"`
	Group   string   `json:"group"`
	Columns []Column `json:"columns"`
	// Hint explains the list to a reader in one sentence.
	Hint string `json:"hint,omitempty"`
	// Info is the longer note behind the small "i" beside the list's title.
	Info string `json:"info,omitempty"`
	// ReadOnly registers are listed here for completeness but written elsewhere (Feed Config).
	ReadOnly bool `json:"read_only,omitempty"`
	// EditHref is where a read-only register's rows are actually edited; EditLabel names it.
	EditHref  string `json:"edit_href,omitempty"`
	EditLabel string `json:"edit_label,omitempty"`
	// Filters names the columns the list offers as filter selects.
	Filters []string `json:"filters,omitempty"`
	// Hidden keeps a register out of the rail: it is still served and written (categories are
	// managed from the Lists panel of Items & categories; feed items are folded into items).
	Hidden bool `json:"hidden,omitempty"`
	// Layout names the page layout for the register: "" (table) or "catalogue" (lists panel +
	// items table, the prototype's Items & categories screen).
	Layout string `json:"layout,omitempty"`
	// ListKey is set on a dynamic reference-list register: the reference_lists row it renders.
	ListKey string `json:"list_key,omitempty"`
	// Importable registers accept a bulk sheet (Configuration -> Import). Every writable
	// register is; a read-only one is not, except Animals, whose sheet is handed to the herd
	// register's own bulk pipeline. Set by init from ReadOnly; only Animals names it itself.
	Importable bool `json:"importable"`
	// DisplayColumn names the column a row's display is taken from when it is not name/label
	// (an animal's primary tag).
	DisplayColumn string `json:"display_column,omitempty"`
	// Unversioned registers keep no row version (shed_partitions has none): their sheet carries 0,
	// and an update from it is applied without a version fence. Every other register refuses 0.
	Unversioned bool `json:"unversioned,omitempty"`
	// ImportCreateOnly registers take new rows from a sheet but never updates (Animals: the herd
	// pipeline creates; an animal is corrected on its own screens).
	ImportCreateOnly bool `json:"import_create_only,omitempty"`
}

// Register keys.
const (
	RegParks      = "parks"
	RegPenTypes   = "pen_types"
	RegPens       = "pens"
	RegPartitions = "partitions"
	RegSpecies    = "species"
	RegBreeds     = "breeds"
	RegSexes      = "sexes"
	RegStages     = "stages"
	RegCategories = "categories"
	RegItems      = "items"
	RegFeedItems  = "feed_items"
	RegRoles      = "roles"
	// RegAnimals is the herd itself, read-only here (edited on the Herd Register) and served so
	// the farm can download it as a sheet and upload new animals in bulk through the herd
	// register's own pipeline.
	RegAnimals = "animals"
	// The static reference registers.
	RegStatusDefinitions = "status_definitions"
	RegSOPCategories     = "sop_categories"
	RegTaskTypes         = "task_types"
	// RegReferenceLists is the register OF lists (hidden from the rail; the "Add list" drawer);
	// each list's entries are a dynamic register keyed RefPrefix + list_key.
	RegReferenceLists = "reference_lists"
	RefPrefix         = "ref:"
)

// ReferenceList is one farm-defined vocabulary (a reference_lists row).
type ReferenceList struct {
	Key         string
	Name        string
	Description string
	SortOrder   int
	IsBuiltin   bool
}

// IsReferenceRegister reports a dynamic reference-list register key.
func IsReferenceRegister(key string) bool {
	return len(key) > len(RefPrefix) && key[:len(RefPrefix)] == RefPrefix
}

// ReferenceListKey is the list a dynamic register key names.
func ReferenceListKey(register string) string {
	if !IsReferenceRegister(register) {
		return ""
	}
	return register[len(RefPrefix):]
}

// ReferenceRegister is the register a reference list renders as: the same entry columns for
// every list, labelled with the list's own name.
func ReferenceRegister(list ReferenceList) Register {
	return Register{
		Key: RefPrefix + list.Key, Label: list.Name, One: "Entry", Group: GroupReference, ListKey: list.Key,
		Hint:       list.Description,
		Importable: true,
		Columns:    referenceEntryColumns,
	}
}

var referenceEntryColumns = []Column{
	{Key: "name", Label: "Name", Type: TypeText, Required: true},
	{Key: "code", Label: "Code", Type: TypeCode, Required: true, Immutable: true, Hint: "Lowercase key; cannot change once saved. Left blank, one is made from the name."},
	{Key: "description", Label: "Description", Type: TypeNotes, ListHidden: true},
	{Key: "sort_order", Label: "Order", Type: TypeNumber, Min: zero(), Integer: true},
}

// RoleGrades is the HR grade a role sits at (designation_catalog.grade, matching
// workforce_members.hr_designation_grade).
var RoleGrades = []Option{
	{Value: "cxo", Label: "CEO / CXO"},
	{Value: "director", Label: "Director"},
	{Value: "head", Label: "Head"},
	{Value: "manager", Label: "Manager"},
	{Value: "assistant_manager", Label: "Assistant manager"},
}

// StageAgeBands are the animal_stage_lookup.age_band values (CHECK age_band IN ('kid','adult')).
var StageAgeBands = []Option{
	{Value: "kid", Label: "Kid"},
	{Value: "adult", Label: "Adult"},
}

// Item kinds are the inventory_items.category enum: every item category root carries one, and
// stock reserve / PC Care requirements / the vaccines detail table still key on it.
var ItemKinds = []Option{
	{Value: "medicine", Label: "Medicine"},
	{Value: "vaccine", Label: "Vaccine"},
	{Value: "dewormer", Label: "Dewormer"},
	{Value: "feed", Label: "Feed"},
	{Value: "supplement", Label: "Supplement"},
	{Value: "consumable", Label: "Consumable"},
	{Value: "other", Label: "Other"},
}

// BuiltinCodes are the codes vaccination, feed and weighing rules still name literally; the
// rows carrying them may be renamed but never archived or deleted (docs/decisions and the
// migration 000346 header).
//
// Stages are keyed lower-case like the rest: K0 is the stage every birth is recorded at, Flushing
// is the flushing cohort, and the others are the rungs of the growth ladder
// (counts/domain.ProductNamedStageCodes, pinned by
// TestProtectedStageCodesCoverEveryStageTheProductNames). Removing one would quietly break births,
// flushing or a growth step, so they can be renamed and re-banded but never archived or deleted.
var BuiltinCodes = map[string]map[string]bool{
	RegSpecies: {"goat": true, "sheep": true},
	RegSexes:   {"female": true, "male": true},
	RegStages: {
		"k0": true, "k1": true, "k2": true, "k3": true,
		"f2": true, "f2-male": true, "f2-female": true, "buck": true,
		"non-pregnant": true, "pregnant": true, "mother": true, "flushing": true,
	},
}

// Departments is the owner department of an item kind, composed by the store on read and
// offered as the items filter (the prototype's "Departments: all").
var Departments = []Option{
	{Value: "preventive_care", Label: "Preventive Care"},
	{Value: "health", Label: "Health"},
	{Value: "feed", Label: "Feed"},
	{Value: "general", Label: "General"},
}

// DepartmentForKind maps an item kind onto the department that owns it.
func DepartmentForKind(kind string) string {
	switch kind {
	case "vaccine":
		return "preventive_care"
	case "medicine", "dewormer", "supplement":
		return "health"
	case "feed":
		return "feed"
	}
	return "general"
}

func zero() *float64 { v := 0.0; return &v }

// Registers is the ordered catalog the rail renders. Order is the rail order.
var Registers = []Register{
	{
		// Roles (maintainer instruction 2026-09-18): the designations a person is given on
		// /people -- CEO / CXO, CTO, a director, a park head -- with the HR grade each sits at.
		// The backing designation_catalog is product-wide, so Configuration can list it but not
		// author it until roles have a tenant-scoped table or a deliberate global-admin surface.
		Key: RegRoles, Label: "Roles", One: "Role", Group: GroupPeople,
		Hint:      "The roles people are given on People / HRMS. Role authoring stays with People / HRMS until roles are tenant-scoped.",
		ReadOnly:  true,
		EditHref:  "/people",
		EditLabel: "People / HRMS",
		Columns: []Column{
			{Key: "name", Label: "Name", Type: TypeText, Required: true},
			{Key: "code", Label: "Code", Type: TypeCode, Required: true, Immutable: true, Hint: "Lowercase key such as cto; cannot change once saved."},
			{Key: "grade", Label: "Grade", Type: TypeEnum, Options: RoleGrades},
			{Key: "sort_order", Label: "Order", Type: TypeNumber, Min: zero(), Integer: true},
		},
	},
	{
		Key: RegParks, Label: "Parks", One: "Park", Group: GroupFarmPlaces,
		// No Farms register (maintainer instruction 2026-09-18): the farm IS the tenant; parks
		// are the top of the place tree on screen.
		Hint: "A park is one site with its own pens, people and work. Animals never move between parks.",
		Columns: []Column{
			{Key: "name", Label: "Name", Type: TypeText, Required: true},
			{Key: "code", Label: "Code", Type: TypeText, Required: true, Hint: "1 to 12 letters and numbers, such as CBE or CPT. It orders parks, names pens and starts every kid's tag, and it cannot change once sales or purchases are recorded under it."},
			{Key: "capacity", Label: "Capacity", Type: TypeNumber, Min: zero(), Integer: true},
			{Key: "notes", Label: "Notes", Type: TypeNotes, ListHidden: true},
		},
	},
	{
		// PEN TYPES ARE THE FARM'S OWN LIST (maintainer instruction 2026-09-25: "after park,
		// before pens, we need pen type ... elevated, non-elevated, whatever we use in weighing or
		// anywhere in future should not be hard coded, it should come from here"). Migration 000437
		// replaced the elevated | non_elevated CHECK with this table and made the partition's
		// shed_type a foreign key into it, so the Partitions dropdown, Weighing's Pen-wise chart and
		// Health's pen-type cut all read one list. The code is the key those readers group by and
		// is immutable; the name is only a label and may be renamed at any time.
		Key: RegPenTypes, Label: "Pen types", One: "Pen type", Group: GroupFarmPlaces,
		Hint: "The kinds of pen the farm builds, such as elevated or non-elevated. Each partition is given one; Weighing and Health Analytics compare them.",
		Columns: []Column{
			{Key: "name", Label: "Name", Type: TypeText, Required: true},
			{Key: "code", Label: "Code", Type: TypeCode, Immutable: true, Hint: "Lowercase key such as elevated; cannot change once saved. Left blank, one is made from the name."},
			{Key: "description", Label: "Description", Type: TypeNotes, ListHidden: true},
			{Key: "sort_order", Label: "Order", Type: TypeNumber, Min: zero(), Integer: true, Hint: "Charts show pen types in this order."},
		},
	},
	{
		// A PEN IS A BUILDING IN A PARK, AND NOTHING ELSE (maintainer instruction 2026-09-22: "no
		// need stage and gender, ICU, all that -- what I keep is my wish; just pens and mapping to
		// park"). Stage, Gender and ICU described what is KEPT in a pen, which is a daily herd
		// decision rather than a setting: the pen's stage is written by the shifting/tag rules and
		// newborn placement, and shed_profiles.sex / has_icu were read by NOTHING outside this
		// screen. They are off the register; the columns stay in the table, so no stored value was
		// lost. PEN TYPE moved DOWN to the partition -- see the Partitions register below.
		Key: RegPens, Label: "Pens", One: "Pen", Group: GroupFarmPlaces,
		Hint:    "A pen is a building in a park. Split it into partitions below when animals are kept apart inside it.",
		Filters: []string{"park_id"},
		Columns: []Column{
			{Key: "park_id", Label: "Park", Type: TypeRef, Ref: RegParks, Required: true},
			{Key: "name", Label: "Name", Type: TypeText, Required: true},
			// CAPACITY MOVED TO THE PARTITION (maintainer instruction 2026-09-30, migration 000464):
			// how many animals a pen holds is set on each partition, never on the building.
			{Key: "notes", Label: "Notes", Type: TypeNotes, ListHidden: true},
		},
	},
	{
		// PEN TYPE IS SET PER PARTITION (maintainer instruction 2026-09-22: "assignment will be per
		// partition only not pen"). A partition IS the pen the farm works -- Castro 1, Mandela 1 -
		// Part 3 -- and one building can hold pens that were built differently, which a single
		// value on the building cannot say. Migration 000391 moved the column down from
		// shed_profiles and carried every already-classified pen with it.
		Key: RegPartitions, Label: "Partitions", One: "Partition", Group: GroupFarmPlaces,
		// shed_partitions has no row_version, so its sheet carries 0; without this a downloaded
		// Partitions sheet could never be uploaded again -- which is how a farm maps a hundred pens
		// to their pen types at once.
		Unversioned: true,
		Hint:        "A partition is one section of a pen, such as Part 3 or 2. Its label is what is painted on the pen.",
		// CAPACITY IS A GUIDE, NOT A LIMIT (maintainer instruction 2026-10-02): a pen may hold more
		// animals than its capacity and nothing anywhere refuses it; the row is shown in red instead.
		// It is set here, on the web, and nowhere on the phone.
		Info:    "Capacity is a guide, not a limit. A partition can hold more animals than its capacity and nothing is refused; when it does, its capacity and animal count show in red. Capacity is set and changed here on the web only.",
		Filters: []string{"park_id", "pen_id", "shed_type"},
		Columns: []Column{
			{Key: "park_id", Label: "Park", Type: TypeRef, Ref: RegParks, Required: true},
			{Key: "pen_id", Label: "Pen", Type: TypeRef, Ref: RegPens, Required: true},
			{Key: "label", Label: "Label", Type: TypeText, Required: true, Hint: "Part 3 and 3 are the same partition."},
			// The choices are the Pen types register (migration 000437), never a list typed here.
			{Key: "shed_type", Label: "Pen type", Type: TypeRef, Ref: RegPenTypes,
				Hint: "Weighing and Health Analytics compare pens by type. Left blank, this pen is reported as unclassified rather than counted into any type."},
			// CAPACITY IS SET PER PARTITION (maintainer instruction 2026-09-30: "it should be per
			// partition capacity, it won't be per pen"). Castro 1 and Castro 2 hold different numbers
			// of animals; one figure on the building could not say so. Migration 000464 moved it here.
			{Key: "capacity", Label: "Capacity", Type: TypeNumber, Min: zero(), Integer: true,
				Hint: "How many animals this partition holds. A pen's capacity is the total of its partitions."},
			{Key: "sort_order", Label: "Order", Type: TypeNumber, Min: zero(), Integer: true},
		},
	},
	{
		Key: RegSpecies, Label: "Species", One: "Species", Group: GroupAnimalTypes,
		Hint: "The kinds of animal the farm keeps. Goat and Sheep are built in: rename them if you like, but they cannot be removed.",
		Columns: []Column{
			{Key: "name", Label: "Name", Type: TypeText, Required: true},
			{Key: "code", Label: "Code", Type: TypeCode, Required: true, Immutable: true, Hint: "Lowercase key such as goat; cannot change once saved."},
			{Key: "sort_order", Label: "Order", Type: TypeNumber, Min: zero(), Integer: true},
		},
	},
	{
		// Breeds (maintainer instruction 2026-09-18; editable per farm since the 2026-09-25
		// decision, migration 000442): the breeds the herd register, the birth form, purchases and
		// sales offer, one row per (farm, species, breed).
		Key: RegBreeds, Label: "Breeds", One: "Breed", Group: GroupAnimalTypes,
		Hint:    "Each breed belongs to a species. Every breed picker on web and phone offers the farm's breeds of the chosen species.",
		Filters: []string{"species"},
		Columns: []Column{
			{Key: "name", Label: "Breed", Type: TypeText, Required: true},
			{Key: "species", Label: "Species", Type: TypeRef, Ref: RegSpecies, Required: true},
			{Key: "notes", Label: "Notes", Type: TypeNotes, ListHidden: true},
		},
	},
	{
		// Labelled "Gender" on screen (maintainer instruction 2026-09-18); the key stays `sexes`
		// and the stored column stays goats.sex -- a vocabulary choice, not a schema one.
		Key: RegSexes, Label: "Gender", One: "Gender", Group: GroupAnimalTypes,
		Hint: "Female and Male are built in and cannot be removed.",
		Columns: []Column{
			{Key: "name", Label: "Name", Type: TypeText, Required: true},
			{Key: "code", Label: "Code", Type: TypeCode, Required: true, Immutable: true},
			{Key: "sort_order", Label: "Order", Type: TypeNumber, Min: zero(), Integer: true},
		},
	},
	{
		Key: RegStages, Label: "Lifecycle stages", One: "Stage", Group: GroupAnimalTypes,
		Hint: "The stages an animal moves through, with the age band each covers. K0, Flushing and the growth stages (K1 to Pregnant, and Mother) are built in: rename them if you like, but they cannot be removed.",
		Columns: []Column{
			{Key: "name", Label: "Name", Type: TypeText, Required: true},
			{Key: "code", Label: "Code", Type: TypeText, Required: true, Immutable: true, Hint: "The tag the farm uses, such as K2 or F2-Male; cannot change once saved."},
			{Key: "min_age_days", Label: "From (days)", Type: TypeNumber, Min: zero(), Integer: true},
			{Key: "max_age_days", Label: "To (days)", Type: TypeNumber, Min: zero(), Integer: true},
			// animal_stage_lookup.age_band (migration 000109): shifting, health routing and the
			// pen retag read it to tell a kid cohort from an adult one. A stage added here without
			// it was unclassified everywhere; left blank it still is, which is right for a clinical
			// state such as ICU.
			{Key: "age_band", Label: "Kid or adult", Type: TypeEnum, Options: StageAgeBands, Hint: "Shifting and Health treat kid and adult stages differently. Leave blank for a clinical state such as ICU."},
			// In the form, not the table: the rows are already listed in this order, and the
			// table needs the room for Kid or adult at laptop width.
			{Key: "sort_order", Label: "Order", Type: TypeNumber, Min: zero(), Integer: true, ListHidden: true},
		},
	},
	{
		Key: RegCategories, Label: "Lists", One: "List", Group: GroupCatalogue, Hidden: true,
		Hint: "Group items any way the farm thinks: Medicines > Antibiotics, Feed > Concentrates. A list behaves like its top-level kind.",
		Columns: []Column{
			{Key: "name", Label: "Name", Type: TypeText, Required: true},
			{Key: "parent_id", Label: "Under", Type: TypeRef, Ref: RegCategories, Hint: "Leave empty for a top-level list."},
			{Key: "kind", Label: "Kind", Type: TypeEnum, Options: ItemKinds, Hint: "Top-level lists only: what the items in it are, for stock and rules."},
			{Key: "sort_order", Label: "Order", Type: TypeNumber, Min: zero(), Integer: true},
		},
	},
	{
		Key: RegItems, Label: "Items & categories", One: "Item", Group: GroupCatalogue, Layout: "catalogue",
		Hint:    "Medicines, vaccines, dewormers, feed and supplies, grouped into the lists on the left.",
		Filters: []string{"category_id", "department"},
		Columns: []Column{
			{Key: "name", Label: "Item", Type: TypeText, Required: true},
			{Key: "department", Label: "Departments", Type: TypeEnum, Derived: true, ListHidden: true, Options: Departments},
			{Key: "tracking", Label: "Tracking", Type: TypeText, Derived: true, ListHidden: true},
			{Key: "category_id", Label: "Category", Type: TypeRef, Ref: RegCategories, Required: true},
			{Key: "code", Label: "Code", Type: TypeText, Hint: "Left blank, one is made from the name."},
			{Key: "unit", Label: "Unit", Type: TypeText, Required: true, Kinds: []string{"medicine", "vaccine", "dewormer", "supplement", "consumable", "other"}, ImpliedOutOfKind: true, Hint: "ml, dose, tablet, kg, piece."},
			{Key: "route", Label: "Route", Type: TypeEnum, Kinds: []string{"medicine", "dewormer"}, Options: []Option{{Value: "im", Label: "IM"}, {Value: "iv", Label: "IV"}, {Value: "sc", Label: "SC"}, {Value: "oral", Label: "Oral"}, {Value: "topical", Label: "Topical"}}},
			{Key: "strength", Label: "Strength", Type: TypeText, Kinds: []string{"medicine", "dewormer"}, Hint: "e.g. 10 mg/ml"},
			{Key: "withdrawal_days", Label: "Withdrawal (days)", Type: TypeNumber, Min: zero(), Integer: true, Kinds: []string{"medicine", "dewormer", "vaccine"}},
			{Key: "disease", Label: "Disease covered", Type: TypeText, Kinds: []string{"vaccine"}},
			{Key: "manufacturer", Label: "Manufacturer", Type: TypeText, Kinds: []string{"vaccine", "medicine", "dewormer"}, ListHidden: true},
			{Key: "doses_per_vial", Label: "Doses per vial", Type: TypeNumber, Min: zero(), Integer: true, Kinds: []string{"vaccine"}},
			{Key: "vaccine_type", Label: "Live / killed", Type: TypeEnum, Kinds: []string{"vaccine"}, Options: []Option{{Value: "live", Label: "Live"}, {Value: "killed", Label: "Killed"}}},
			// The three numbers the ration maths reads. They sit beside the feed's name now that a
			// feed item is added and removed here rather than on Feed Config.
			{Key: "energy_kcal_per_kg", Label: "Energy (kcal/kg)", Type: TypeNumber, Kinds: []string{"feed"}, Min: zero(), ListHidden: true},
			{Key: "dry_matter_factor", Label: "Dry matter", Type: TypeNumber, Kinds: []string{"feed"}, Min: zero(), ListHidden: true, Hint: "Between 0 and 1."},
			{Key: "wastage_factor", Label: "Wastage", Type: TypeNumber, Kinds: []string{"feed"}, Min: zero(), ListHidden: true, Hint: "Between 0 and 1."},
			{Key: "notes", Label: "Notes", Type: TypeNotes, ListHidden: true},
		},
	},
	{
		// Kept hidden and read-only as a listing of its own; a feed item is ADDED, EDITED and
		// REMOVED on Items & categories under Feed (maintainer instruction 2026-09-22), not here
		// and no longer on Feed Config.
		Key: RegFeedItems, Label: "Feed items", One: "Feed item", Group: GroupCatalogue, ReadOnly: true, Hidden: true, EditHref: "/configuration/items", EditLabel: "Items & categories",
		Hint: "What the farm feeds. Added and removed under Items & categories -> Feed.",
		Columns: []Column{
			{Key: "name", Label: "Name", Type: TypeText, Required: true},
			{Key: "energy_kcal_per_kg", Label: "Energy (kcal/kg)", Type: TypeNumber},
			{Key: "dry_matter_factor", Label: "Dry matter", Type: TypeNumber},
			{Key: "wastage_factor", Label: "Wastage", Type: TypeNumber},
		},
	},
	{
		// Reference lists (maintainer instruction 2026-09-18). Status definitions are the axes
		// of an animal's state (lifecycle, reproductive, health, growth cohort, management), a
		// product-wide catalog whose codes Go names, so they are read-only on this tenant screen.
		Key: RegStatusDefinitions, Label: "Status definitions", One: "Status", Group: GroupReference,
		Hint:      "The states an animal can be in, by axis. Codes are built into the product and edited only through a product-wide release.",
		ReadOnly:  true,
		EditHref:  "/configuration/items",
		EditLabel: "Product release",
		Filters:   []string{"axis"},
		Columns: []Column{
			{Key: "name", Label: "Name", Type: TypeText, Required: true},
			{Key: "axis", Label: "Axis", Type: TypeEnum, Required: true, Options: []Option{{Value: "lifecycle", Label: "Lifecycle"}, {Value: "reproductive", Label: "Reproductive"}, {Value: "growth_cohort", Label: "Growth cohort"}, {Value: "management", Label: "Management"}, {Value: "health", Label: "Health"}}},
			{Key: "code", Label: "Code", Type: TypeText, Required: true, Immutable: true},
			{Key: "short_label", Label: "Short label", Type: TypeText, Required: true},
			{Key: "description", Label: "Description", Type: TypeNotes, ListHidden: true},
			{Key: "expected_duration_days", Label: "Expected days", Type: TypeNumber, Min: zero(), Integer: true},
			{Key: "sort_order", Label: "Order", Type: TypeNumber, Min: zero(), Integer: true},
		},
	},
	{
		Key: RegSOPCategories, Label: "SOP categories", One: "SOP category", Group: GroupReference,
		Hint: "How work instructions are grouped: commodity, problem, event, action, equipment.",
		Columns: []Column{
			{Key: "name", Label: "Name", Type: TypeText, Required: true},
			{Key: "code", Label: "Code", Type: TypeCode, Required: true, Immutable: true},
			{Key: "description", Label: "Description", Type: TypeNotes, ListHidden: true},
			{Key: "sort_order", Label: "Order", Type: TypeNumber, Min: zero(), Integer: true},
		},
	},
	{
		Key: RegTaskTypes, Label: "Task types", One: "Task type", Group: GroupReference,
		Hint:    "The kinds of step a work instruction can ask for, and what the operator answers.",
		Filters: []string{"answer_kind"},
		Columns: []Column{
			{Key: "name", Label: "Name", Type: TypeText, Required: true},
			{Key: "code", Label: "Code", Type: TypeCode, Required: true, Immutable: true},
			{Key: "answer_kind", Label: "Answer", Type: TypeEnum, Required: true, Options: []Option{{Value: "none", Label: "None"}, {Value: "yes_no", Label: "Yes / no"}, {Value: "select", Label: "Pick one"}, {Value: "multiselect", Label: "Pick many"}, {Value: "number", Label: "Number"}, {Value: "text", Label: "Text"}}},
			{Key: "description", Label: "Description", Type: TypeNotes, ListHidden: true},
			{Key: "sort_order", Label: "Order", Type: TypeNumber, Min: zero(), Integer: true},
		},
	},
	{
		// Animals (maintainer instruction 2026-09-18, "one lakh animals"): the herd, listed so it
		// can be exported as a sheet and imported in bulk. Rows are written by the herd register's
		// bulk pipeline (identity), never by a configuration store; the screen links there.
		Key: RegAnimals, Label: "Animals", One: "Animal", Group: GroupAnimals, ReadOnly: true, Importable: true, ImportCreateOnly: true, DisplayColumn: "animal_identifier_1",
		EditHref: "/counts/herd", EditLabel: "Herd Register",
		Hint:    "Every animal on the register. Download the sheet, or upload new animals in bulk; one animal is edited on the Herd Register.",
		Filters: []string{"species", "sex"},
		Columns: []Column{
			{Key: "animal_identifier_1", Label: "RFID / tag 1", Type: TypeText, Required: true, Hint: "The animal's primary tag."},
			{Key: "animal_identifier_2", Label: "Tag 2", Type: TypeText, ListHidden: true},
			{Key: "species", Label: "Species", Type: TypeRef, Ref: RegSpecies, Required: true, Hint: "A species from Animal types: its name or code."},
			{Key: "breed", Label: "Breed", Type: TypeText},
			{Key: "sex", Label: "Gender", Type: TypeRef, Ref: RegSexes, Required: true, Hint: "Female or male."},
			{Key: "park", Label: "Park", Type: TypeText, Required: true, Hint: "The park code, such as CBE."},
			{Key: "pen_name", Label: "Pen", Type: TypeText, Required: true, Hint: "The pen's name in that park."},
			{Key: "partition_label", Label: "Partition", Type: TypeText, Hint: "Required when the pen is partitioned: Part 3, or 3."},
			{Key: "management_stage", Label: "Stage", Type: TypeText, Hint: "A lifecycle stage code, such as K2."},
			{Key: "dob", Label: "Date of birth", Type: TypeText, Hint: "YYYY-MM-DD."},
			{Key: "entry_date", Label: "Entry date", Type: TypeText, Hint: "YYYY-MM-DD; today when left blank."},
			{Key: "origin", Label: "Origin", Type: TypeText, Required: true, Hint: "birth, procured or imported."},
			{Key: "reproductive_status", Label: "Reproductive", Type: TypeText, ListHidden: true},
			{Key: "weight_kg", Label: "Weight (kg)", Type: TypeNumber, ListHidden: true, Min: zero()},
			{Key: "display_id", Label: "Display id", Type: TypeText, Derived: true},
			{Key: "lifecycle_status", Label: "Status", Type: TypeText, Derived: true},
		},
	},
	{
		Key: RegReferenceLists, Label: "Reference lists", One: "List", Group: GroupReference, Hidden: true,
		Hint: "A list of your own: name it, then add its entries.",
		Columns: []Column{
			{Key: "name", Label: "Name", Type: TypeText, Required: true},
			{Key: "code", Label: "Code", Type: TypeCode, Required: true, Immutable: true, Hint: "Lowercase key; cannot change once saved."},
			{Key: "description", Label: "Description", Type: TypeNotes},
			{Key: "sort_order", Label: "Order", Type: TypeNumber, Min: zero(), Integer: true},
		},
	},
}

var registerIndex = func() map[string]Register {
	out := make(map[string]Register, len(Registers))
	for _, r := range Registers {
		if _, dup := out[r.Key]; dup {
			panic("configuration: duplicate register " + r.Key)
		}
		for _, c := range r.Columns {
			if c.Type == TypeRef && c.Ref == "" {
				panic("configuration: ref column without a target: " + r.Key + "." + c.Key)
			}
			if c.Type == TypeEnum && len(c.Options) == 0 {
				panic("configuration: enum column without options: " + r.Key + "." + c.Key)
			}
		}
		out[r.Key] = r
	}
	// Every writable register takes a sheet; a read-only one only when it says so (Animals).
	for i := range Registers {
		if !Registers[i].ReadOnly {
			Registers[i].Importable = true
		}
		out[Registers[i].Key] = Registers[i]
	}
	for _, r := range Registers {
		for _, c := range r.Columns {
			if c.Type == TypeRef {
				if _, ok := out[c.Ref]; !ok {
					panic("configuration: ref column points at unknown register: " + r.Key + "." + c.Key)
				}
			}
		}
	}
	return out
}()

// RegisterByKey resolves a register; ok is false for an unknown key. A dynamic reference-list
// key resolves to the entry template labelled with its key; callers that know the list
// substitute the list's name through ReferenceRegister.
func RegisterByKey(key string) (Register, bool) {
	if IsReferenceRegister(key) {
		return ReferenceRegister(ReferenceList{Key: ReferenceListKey(key), Name: ReferenceListKey(key)}), true
	}
	r, ok := registerIndex[key]
	return r, ok
}

// Column resolves one column of the register.
func (r Register) Column(key string) (Column, bool) {
	for _, c := range r.Columns {
		if c.Key == key {
			return c, true
		}
	}
	return Column{}, false
}

// IsBuiltinCode reports whether a register's code is one the rest of the product names literally.
// Codes compare case-insensitively: stage codes are authored mixed-case (K0, F2-Male).
func IsBuiltinCode(register, code string) bool {
	return BuiltinCodes[register][strings.ToLower(strings.TrimSpace(code))]
}
