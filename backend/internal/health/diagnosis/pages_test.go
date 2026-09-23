package diagnosis

import "testing"

// Pages are how the operator walks the form. They were a four-value Kotlin enum, so moving a
// question between pages was a deploy; they are authored now.

// An existing register -- every one shipped before pages were authorable -- has no sections, and
// must still walk in the order it was laid out. The form follows a person's hands down the animal;
// sorting the pages would scatter that.
func TestPagesFallBackToDeclarationOrderWhenNoneAreAuthored(t *testing.T) {
	doc := AuthoredRegister{Questions: []Question{
		{ID: "temp", Section: "Vitals"},
		{ID: "eyes", Section: "Head"},
		{ID: "pulse", Section: "Vitals"},
		{ID: "gut", Section: "Body"},
	}}

	pages := doc.Pages()
	if len(pages) != 3 {
		t.Fatalf("want 3 pages, got %d", len(pages))
	}
	for i, want := range []string{"Vitals", "Head", "Body"} {
		if pages[i].ID != want {
			t.Errorf("page %d is %q, want %q -- declaration order, not alphabetical", i, pages[i].ID, want)
		}
	}
	if len(pages[0].Questions) != 2 {
		t.Errorf("Vitals holds %d questions, want both of its own", len(pages[0].Questions))
	}
}

// Authored sections decide the order, which is the whole point: re-ordering pages is an edit.
func TestAuthoredSectionsDecideTheOrder(t *testing.T) {
	doc := AuthoredRegister{
		Sections: []Section{{ID: "body", Title: "Gut"}, {ID: "vitals", Title: "Vitals"}},
		Questions: []Question{
			{ID: "temp", Section: "vitals"},
			{ID: "gut", Section: "body"},
		},
	}

	pages := doc.Pages()
	if len(pages) != 2 || pages[0].ID != "body" || pages[1].ID != "vitals" {
		t.Fatalf("authored order must win, got %+v", pages)
	}
	if pages[0].Title != "Gut" {
		t.Errorf("the page shows its authored title, got %q", pages[0].Title)
	}
}

// A question naming no authored page is APPENDED, never dropped. A question that exists and is
// never asked is the silent accept-and-discard this codebase refuses everywhere else -- and on an
// observation form it means a sign nobody records.
func TestAQuestionWithAnUnknownSectionIsStillAsked(t *testing.T) {
	doc := AuthoredRegister{
		Sections:  []Section{{ID: "vitals", Title: "Vitals"}},
		Questions: []Question{{ID: "temp", Section: "vitals"}, {ID: "udder", Section: "udder"}},
	}

	pages := doc.Pages()
	asked := 0
	for _, p := range pages {
		asked += len(p.Questions)
	}
	if asked != 2 {
		t.Fatalf("every question must be asked somewhere, got %d of 2", asked)
	}
}

// A section nobody put a question on is not shown: an empty page with a Next button is a step that
// does nothing, and an authored section can outlive the last question that named it.
func TestAnEmptyAuthoredPageIsNotShown(t *testing.T) {
	doc := AuthoredRegister{
		Sections:  []Section{{ID: "vitals", Title: "Vitals"}, {ID: "ghost", Title: "Ghost"}},
		Questions: []Question{{ID: "temp", Section: "vitals"}},
	}
	if pages := doc.Pages(); len(pages) != 1 || pages[0].ID != "vitals" {
		t.Errorf("want only the page that has questions, got %+v", pages)
	}
}

// Every shipped register still pages, which is the deploy-day property: adding sections changed
// nothing for a farm that has not authored any.
func TestEverySeededRegisterStillPages(t *testing.T) {
	for _, class := range Classes {
		doc, err := SeedAuthored(class, nil)
		if err != nil {
			t.Fatalf("seed %s: %v", class, err)
		}
		pages := doc.Pages()
		if len(pages) == 0 {
			t.Errorf("%s pages to nothing", class)
		}
		asked := 0
		for _, p := range pages {
			asked += len(p.Questions)
		}
		if asked != len(doc.Questions) {
			t.Errorf("%s asks %d of its %d questions", class, asked, len(doc.Questions))
		}
	}
}
