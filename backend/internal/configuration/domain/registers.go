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
}

// Register keys.
const (
	RegParks      = "parks"
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
		Hint:    list.Description,
		Columns: referenceEntryColumns,
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
var BuiltinCodes = map[string]map[string]bool{
	RegSpecies: {"goat": true, "sheep": true},
	RegSexes:   {"female": true, "male": true},
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
		// Backed by designation_catalog, the same catalog /people offers, so a role added here is
		// offered there at once. What a role may DO is still the per-person ticks on /people.
		Key: RegRoles, Label: "Roles", One: "Role", Group: GroupPeople,
		Hint: "The roles people are given on People / HRMS, and the grade each sits at. What a role may do is set per person there.",
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
			{Key: "code", Label: "Code", Type: TypeText, Required: true, Hint: "Short code such as CBE or CPT; used to order parks and name pens."},
			{Key: "capacity", Label: "Capacity", Type: TypeNumber, Min: zero(), Integer: true},
			{Key: "notes", Label: "Notes", Type: TypeNotes, ListHidden: true},
		},
	},
	{
		Key: RegPens, Label: "Pens", One: "Pen", Group: GroupFarmPlaces,
		Hint:    "A pen is a building in a park. Split it into partitions below when animals are kept apart inside it.",
		Filters: []string{"park_id"},
		Columns: []Column{
			{Key: "park_id", Label: "Park", Type: TypeRef, Ref: RegParks, Required: true},
			{Key: "name", Label: "Name", Type: TypeText, Required: true},
			{Key: "capacity", Label: "Capacity", Type: TypeNumber, Min: zero(), Integer: true},
			{Key: "stage_id", Label: "Stage", Type: TypeRef, Ref: RegStages, Hint: "The lifecycle stage the pen is kept for, when it has one."},
			{Key: "sex", Label: "Gender", Type: TypeEnum, Options: []Option{{Value: "mixed", Label: "Mixed"}, {Value: "female", Label: "Female"}, {Value: "male", Label: "Male"}}},
			{Key: "has_icu", Label: "ICU", Type: TypeBool},
			{Key: "notes", Label: "Notes", Type: TypeNotes, ListHidden: true},
		},
	},
	{
		Key: RegPartitions, Label: "Partitions", One: "Partition", Group: GroupFarmPlaces,
		Hint:    "A partition is one section of a pen, such as Part 3 or 2. Its label is what is painted on the pen.",
		Filters: []string{"park_id", "pen_id"},
		Columns: []Column{
			{Key: "park_id", Label: "Park", Type: TypeRef, Ref: RegParks, Required: true},
			{Key: "pen_id", Label: "Pen", Type: TypeRef, Ref: RegPens, Required: true},
			{Key: "label", Label: "Label", Type: TypeText, Required: true, Hint: "Part 3 and 3 are the same partition."},
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
		// Breeds (maintainer instruction 2026-09-18): the breeds table the herd register and
		// procurement already name, one row per (species, breed). 'review' rows -- a breed the
		// importer saw but nobody confirmed -- show as archived until someone restores them.
		Key: RegBreeds, Label: "Breeds", One: "Breed", Group: GroupAnimalTypes,
		Hint:    "The breeds each species comes in. A breed still carried by animals cannot be removed.",
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
		Hint: "The stages an animal moves through, with the age band each covers.",
		Columns: []Column{
			{Key: "name", Label: "Name", Type: TypeText, Required: true},
			{Key: "code", Label: "Code", Type: TypeText, Required: true, Immutable: true, Hint: "The tag the farm uses, such as K2 or F2-Male; cannot change once saved."},
			{Key: "min_age_days", Label: "From (days)", Type: TypeNumber, Min: zero(), Integer: true},
			{Key: "max_age_days", Label: "To (days)", Type: TypeNumber, Min: zero(), Integer: true},
			{Key: "sort_order", Label: "Order", Type: TypeNumber, Min: zero(), Integer: true},
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
			{Key: "unit", Label: "Unit", Type: TypeText, Required: true, Hint: "ml, dose, tablet, kg, piece."},
			{Key: "route", Label: "Route", Type: TypeEnum, Kinds: []string{"medicine", "dewormer"}, Options: []Option{{Value: "im", Label: "IM"}, {Value: "iv", Label: "IV"}, {Value: "sc", Label: "SC"}, {Value: "oral", Label: "Oral"}, {Value: "topical", Label: "Topical"}}},
			{Key: "strength", Label: "Strength", Type: TypeText, Kinds: []string{"medicine", "dewormer"}, Hint: "e.g. 10 mg/ml"},
			{Key: "withdrawal_days", Label: "Withdrawal (days)", Type: TypeNumber, Min: zero(), Integer: true, Kinds: []string{"medicine", "dewormer", "vaccine"}},
			{Key: "disease", Label: "Disease covered", Type: TypeText, Kinds: []string{"vaccine"}},
			{Key: "manufacturer", Label: "Manufacturer", Type: TypeText, Kinds: []string{"vaccine", "medicine", "dewormer"}, ListHidden: true},
			{Key: "doses_per_vial", Label: "Doses per vial", Type: TypeNumber, Min: zero(), Integer: true, Kinds: []string{"vaccine"}},
			{Key: "vaccine_type", Label: "Live / killed", Type: TypeEnum, Kinds: []string{"vaccine"}, Options: []Option{{Value: "live", Label: "Live"}, {Value: "killed", Label: "Killed"}}},
			{Key: "notes", Label: "Notes", Type: TypeNotes, ListHidden: true},
		},
	},
	{
		Key: RegFeedItems, Label: "Feed items", One: "Feed item", Group: GroupCatalogue, ReadOnly: true, Hidden: true, EditHref: "/feed/config", EditLabel: "Feed Config",
		Hint: "What the farm feeds. Edited in Feed Config, where the ration grid depends on it.",
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
		// product-wide catalog whose codes Go names: rename only.
		Key: RegStatusDefinitions, Label: "Status definitions", One: "Status", Group: GroupReference,
		Hint:    "The states an animal can be in, by axis. Codes are built into the product; names can change.",
		Filters: []string{"axis"},
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
func IsBuiltinCode(register, code string) bool {
	return BuiltinCodes[register][code]
}
