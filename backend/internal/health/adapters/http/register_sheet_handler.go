package http

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// THE REGISTER AS A SHEET: download a type's rulebook, edit it, upload it back.
//
// Maintainer instruction 2026-09-23. Forty questions and thirty illnesses typed one field at a
// time through a web form is how a new type stays half-authored for a month.
//
// AN UPLOAD WRITES A DRAFT. It never publishes, and that is the whole safety design rather than a
// limitation: the draft lands on the Diagnosis tab where the author reads it, and publishing runs
// the same two-direction check every hand edit runs -- a question no rule reads, a rule reading a
// finding no question asks. A sheet that skipped that gate could put a rule table nobody can be
// diagnosed against in front of the herd.
//
// The IMPORT IS SYNCHRONOUS, unlike the configuration registers' background job. A register is
// forty questions and thirty illnesses -- a few hundred rows, tens of kilobytes -- so a job, a
// poll and a progress count would be machinery around a request that finishes in under a second.
// The configuration importer's shape is right for a herd of animals and wrong for this.

const (
	registerTemplateRoute = "/health-config/registers/{animal_class}/template"
	registerExportRoute   = "/health-config/registers/{animal_class}/export"
	registerImportRoute   = "/health-config/registers/{animal_class}/import"

	importRegisterSheetCommand = "healthconfig.register.sheet.import"

	// A sheet is small. The cap is here so a mis-aimed upload fails as a refusal rather than as
	// memory pressure, and it is generous enough for a register several times the size of any
	// this farm has.
	maxSheetBytes = 8 << 20
)

// RegisterSheetService is the handler's view of what a sheet needs.
type RegisterSheetService interface {
	GetDraftForEdit(ctx context.Context, cmd domain.RegisterVersionCommand, animalClass string) (domain.RegisterDetail, error)
	SaveDraft(ctx context.Context, cmd domain.SaveRegisterDraftCommand) (domain.RegisterAuthoringResult, error)
	PublishedRegisterDocument(ctx context.Context, tenantID, animalClass string) (diagnosis.AuthoredRegister, error)
}

func RegisterRegisterSheets(mux *http.ServeMux, h *RegisterSheetHandler) {
	mux.HandleFunc("GET "+registerTemplateRoute, h.Template)
	mux.HandleFunc("GET "+registerExportRoute, h.Export)
	mux.HandleFunc("POST "+registerImportRoute, h.Import)
}

type RegisterSheetHandler struct {
	svc RegisterSheetService
	log *slog.Logger
}

func NewRegisterSheetHandler(svc RegisterSheetService, log *slog.Logger) *RegisterSheetHandler {
	if log == nil {
		log = slog.Default()
	}
	return &RegisterSheetHandler{svc: svc, log: log}
}

// Template serves an EMPTY sheet: the header, plus one commented example of each row kind.
//
// The examples matter more than the header. A person handed forty blank columns cannot tell that
// an answer belongs under its question, or that a semicolon separates findings inside a cell; a
// person handed one filled row of each kind can see the whole shape at a glance. They are marked
// with a leading `#` in the `row` column, which the decoder skips as an unknown kind -- so a
// template uploaded unchanged is refused with a sentence rather than silently importing examples.
func (h *RegisterSheetHandler) Template(w http.ResponseWriter, r *http.Request) {
	rows := [][]string{append([]string{}, diagnosis.SheetColumns...)}
	rows = append(rows, diagnosis.SheetExampleRows()...)
	h.writeSheet(w, r, rows, "diagnosis-register-template")
}

// Export serves this type's LIVE register as a sheet -- the usual starting point, because editing
// what the farm already has beats retyping it.
func (h *RegisterSheetHandler) Export(w http.ResponseWriter, r *http.Request) {
	class := r.PathValue("animal_class")
	doc, err := h.svc.PublishedRegisterDocument(r.Context(),
		httpmiddleware.TenantIDFromContext(r.Context()), class)
	if err != nil {
		h.writeSheetError(w, r, err)
		return
	}
	// THE DOCUMENT ITSELF, for a system that will read it back rather than a person who will
	// scroll it -- and it round-trips with the JSON upload. A workbook is for a vet, a CSV for a
	// script, this for a tool that already holds the structure and would only lose it by
	// flattening into rows.
	if strings.EqualFold(r.URL.Query().Get("format"), "json") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="diagnosis-register-`+class+`.json"`)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(doc); err != nil {
			h.log.Error("write register json", "error", err)
		}
		return
	}
	rows, err := diagnosis.EncodeSheet(doc)
	if err != nil {
		h.writeSheetError(w, r, err)
		return
	}
	h.writeSheet(w, r, rows, "diagnosis-register-"+class)
}

// Import parses an uploaded sheet into this type's DRAFT.
//
// Nothing is written until the whole sheet parses: a half-imported register is a rule table with
// some of its illnesses missing, which is worse than the one it replaced.
func (h *RegisterSheetHandler) Import(w http.ResponseWriter, r *http.Request) {
	class := r.PathValue("animal_class")
	idem, ok := (&ConfigHandler{log: h.log}).idempotencyKey(w, r)
	if !ok {
		return
	}

	if err := r.ParseMultipartForm(maxSheetBytes); err != nil {
		h.writeErr(w, r, http.StatusBadRequest, "sheet_unreadable", "That file could not be read.", err)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		h.writeErr(w, r, http.StatusBadRequest, "sheet_missing", "Choose a sheet to upload.", err)
		return
	}
	defer file.Close()

	// JSON IS THE REGISTER ITSELF, not a third spelling of the sheet.
	//
	// The three formats answer different jobs. A vet edits a workbook; a script writes a CSV; a
	// SYSTEM -- another tool, an export from somewhere else, a generated 300-question draft --
	// emits the document. Making the last one go through the row grammar would mean flattening a
	// structure it already has, and flattening loses the very nesting the grammar exists to
	// rebuild.
	var doc *diagnosis.AuthoredRegister
	var problems []string
	if strings.HasSuffix(strings.ToLower(header.Filename), ".json") {
		doc, err = readRegisterJSON(file)
		if err != nil {
			h.writeErr(w, r, http.StatusUnprocessableEntity, "sheet_unreadable", err.Error(), err)
			return
		}
	} else {
		rows, readErr := readSheet(file, header.Filename)
		if readErr != nil {
			h.writeErr(w, r, http.StatusUnprocessableEntity, "sheet_unreadable", readErr.Error(), readErr)
			return
		}
		doc, problems = diagnosis.DecodeSheet(rows)
	}
	if len(problems) > 0 {
		// Every problem at once. A person who has just filled three hundred rows and is told
		// about one mistake per upload will be at it all afternoon.
		httpresponse.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"code": "sheet_problems", "message": "This sheet cannot be read yet.",
			"problems": problems,
		})
		return
	}

	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	actorID := httpmiddleware.ActorIDFromContext(r.Context())

	// Opening the draft first is what makes an import repeatable: the second upload of a
	// corrected sheet replaces the same draft rather than colliding with it.
	if _, err := h.svc.GetDraftForEdit(r.Context(), domain.RegisterVersionCommand{
		TenantID: tenantID, ActorID: actorID,
		IdempotencyKey:     idem + ":open",
		RequestFingerprint: configFingerprint(importRegisterSheetCommand, []byte(class)),
	}, class); err != nil && !errors.Is(err, ports.ErrRegisterDraftExists) {
		h.writeSheetError(w, r, err)
		return
	}

	result, err := h.svc.SaveDraft(r.Context(), domain.SaveRegisterDraftCommand{
		TenantID: tenantID, ActorID: actorID,
		IdempotencyKey:     idem,
		RequestFingerprint: configFingerprint(importRegisterSheetCommand, []byte(class)),
		AnimalClass:        class,
		Document:           *doc,
	})
	if err != nil {
		h.writeSheetError(w, r, err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, map[string]any{
		"outcome":   result.Outcome,
		"questions": len(doc.Questions),
		"rules":     len(doc.Rules),
		// The warnings a publish WOULD allow through, surfaced now: a question no rule reads, an
		// illness whose treatment course nobody has written. Telling an author after they publish
		// is telling them too late.
		"warnings": doc.Validate(),
	})
}

// readRegisterJSON reads the register document itself.
//
// UNKNOWN FIELDS ARE REFUSED. `encoding/json` drops what it does not recognise in silence, so a
// misspelled key would upload a register missing a whole section of rules and report success --
// the loader defect this repo has a standing rule about. A typo is named instead.
func readRegisterJSON(file io.Reader) (*diagnosis.AuthoredRegister, error) {
	dec := json.NewDecoder(io.LimitReader(file, maxSheetBytes))
	dec.DisallowUnknownFields()
	var doc diagnosis.AuthoredRegister
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("that JSON could not be read: %v", err)
	}
	if len(doc.Questions) == 0 {
		return nil, fmt.Errorf("that JSON has no questions in it")
	}
	return &doc, nil
}

// readSheet accepts CSV or XLSX, chosen by the file's own name.
//
// Both, because the farm's own sheets arrive both ways: a vet exports to Excel, a script writes a
// CSV. Refusing one would mean somebody converting a file by hand before every upload, which is a
// step that eventually gets skipped.
func readSheet(file io.Reader, filename string) ([][]string, error) {
	if strings.HasSuffix(strings.ToLower(filename), ".xlsx") {
		data, err := io.ReadAll(io.LimitReader(file, maxSheetBytes))
		if err != nil {
			return nil, fmt.Errorf("that Excel file could not be read: %w", err)
		}
		rows, err := readXLSXRows(data)
		if err != nil {
			return nil, fmt.Errorf("that Excel file could not be opened: %w", err)
		}
		return rows, nil
	}
	reader := csv.NewReader(io.LimitReader(file, maxSheetBytes))
	reader.FieldsPerRecord = -1 // a hand-edited sheet has ragged rows; the decoder reads by header
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("that CSV could not be read: %v", err)
	}
	return rows, nil
}

func readXLSXRows(data []byte) ([][]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		files[f.Name] = f
	}
	shared, err := readXLSXSharedStrings(files["xl/sharedStrings.xml"])
	if err != nil {
		return nil, err
	}
	sheetPath, err := firstXLSXSheetPath(files)
	if err != nil {
		return nil, err
	}
	return readXLSXSheet(files[sheetPath], shared)
}

func firstXLSXSheetPath(files map[string]*zip.File) (string, error) {
	type sheet struct {
		ID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
	}
	type workbook struct {
		Sheets []sheet `xml:"sheets>sheet"`
	}
	wbFile := files["xl/workbook.xml"]
	if wbFile == nil {
		return "", fmt.Errorf("workbook.xml is missing")
	}
	var wb workbook
	if err := decodeZipXML(wbFile, &wb); err != nil {
		return "", err
	}
	if len(wb.Sheets) == 0 {
		return "", fmt.Errorf("that workbook has no sheets in it")
	}
	type rel struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
	}
	var rels struct {
		Rels []rel `xml:"Relationship"`
	}
	if err := decodeZipXML(files["xl/_rels/workbook.xml.rels"], &rels); err != nil {
		return "", err
	}
	for _, rel := range rels.Rels {
		if rel.ID == wb.Sheets[0].ID {
			target := strings.TrimPrefix(rel.Target, "/")
			if !strings.HasPrefix(target, "xl/") {
				target = "xl/" + strings.TrimPrefix(target, "./")
			}
			if files[target] == nil {
				return "", fmt.Errorf("first worksheet is missing")
			}
			return target, nil
		}
	}
	return "", fmt.Errorf("first worksheet relationship is missing")
}

func readXLSXSharedStrings(f *zip.File) ([]string, error) {
	if f == nil {
		return nil, nil
	}
	var doc struct {
		Items []struct {
			Texts []string `xml:"t"`
		} `xml:"si"`
	}
	if err := decodeZipXML(f, &doc); err != nil {
		return nil, err
	}
	out := make([]string, len(doc.Items))
	for i, item := range doc.Items {
		out[i] = strings.Join(item.Texts, "")
	}
	return out, nil
}

func readXLSXSheet(f *zip.File, shared []string) ([][]string, error) {
	if f == nil {
		return nil, fmt.Errorf("first worksheet is missing")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var rows [][]string
	var row []string
	var cellType, cellRef, text string
	inV := false
	dec := xml.NewDecoder(rc)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				row = nil
			case "c":
				cellType, cellRef, text = "", "", ""
				for _, attr := range t.Attr {
					switch attr.Name.Local {
					case "t":
						cellType = attr.Value
					case "r":
						cellRef = attr.Value
					}
				}
			case "v", "t":
				inV = true
				text = ""
			}
		case xml.CharData:
			if inV {
				text += string(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v", "t":
				inV = false
			case "c":
				col := xlsxColumnIndex(cellRef)
				for len(row) <= col {
					row = append(row, "")
				}
				if cellType == "s" {
					var idx int
					if _, err := fmt.Sscanf(text, "%d", &idx); err == nil && idx >= 0 && idx < len(shared) {
						text = shared[idx]
					}
				}
				row[col] = text
			case "row":
				rows = append(rows, row)
			}
		}
	}
}

func xlsxColumnIndex(ref string) int {
	col := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		col = col*26 + int(r-'A'+1)
	}
	if col == 0 {
		return 0
	}
	return col - 1
}

func decodeZipXML(f *zip.File, v any) error {
	if f == nil {
		return fmt.Errorf("xlsx part is missing")
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return xml.NewDecoder(rc).Decode(v)
}

func (h *RegisterSheetHandler) writeSheet(w http.ResponseWriter, r *http.Request, rows [][]string, stem string) {
	if strings.EqualFold(r.URL.Query().Get("format"), "xlsx") {
		f := excelize.NewFile()
		defer f.Close()
		sheet := f.GetSheetName(0)
		for i, row := range rows {
			cell, _ := excelize.CoordinatesToCellName(1, i+1)
			if err := f.SetSheetRow(sheet, cell, &row); err != nil {
				h.writeErr(w, r, http.StatusInternalServerError, "internal_error", "Something went wrong.", err)
				return
			}
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", `attachment; filename="`+stem+`.xlsx"`)
		if err := f.Write(w); err != nil {
			h.log.Error("write register xlsx", "error", err)
		}
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+stem+`.csv"`)
	cw := csv.NewWriter(w)
	for _, row := range rows {
		safe := make([]string, len(row))
		for i, cell := range row {
			safe[i] = csvSafeCell(cell)
		}
		if err := cw.Write(safe); err != nil {
			h.log.Error("write register csv", "error", err)
			return
		}
	}
	cw.Flush()
}

// csvSafeCell defuses spreadsheet formula injection.
//
// A cell opening with =, +, - or @ is EXECUTED by Excel when the file is opened, and these cells
// carry author-supplied text. Prefixing a tab leaves the value readable and identical on the way
// back in, because the decoder trims whitespace.
func csvSafeCell(cell string) string {
	if cell == "" {
		return ""
	}
	switch cell[0] {
	case '=', '+', '-', '@':
		return "\t" + cell
	}
	return cell
}

func (h *RegisterSheetHandler) writeErr(w http.ResponseWriter, r *http.Request, status int, code, msg string, cause error) {
	(&ConfigHandler{log: h.log}).writeError(w, r, status, code, msg, cause)
}

func (h *RegisterSheetHandler) writeSheetError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ports.ErrRegisterNotFound):
		h.writeErr(w, r, http.StatusNotFound, "not_found",
			"This type has no published rules yet, so there is nothing to download. Start from the template.", err)
	default:
		(&RegisterConfigHandler{log: h.log}).writeRegisterError(w, r, err)
	}
}
