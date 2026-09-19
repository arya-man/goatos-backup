package app

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A minimal streaming XLSX READER on the standard library. Uploads are read here rather than
// through excelize's row reader, whose shared-string path carries an unfixed advisory
// (GO-2026-6452: a crafted index panics the reader); a sheet a person uploads is untrusted
// input, so the reader must not be able to take the API down. Writing still uses excelize's
// stream writer (2.11.0, clear).
//
// What it reads, which is what a spreadsheet saved from Excel, Numbers or LibreOffice carries:
// every worksheet in workbook order (a single-sheet upload reads the first); shared strings (rich-text runs joined); inline strings;
// booleans; numbers, rendered as written; and dates -- a numeric cell under a date number format
// becomes YYYY-MM-DD, so a date-of-birth typed in Excel arrives as the register expects. Rows
// stream one at a time; only the shared-string table is held (a few MB for a large sheet).

type xlsxWorkbook struct {
	sheetXML  io.ReadCloser
	dec       *xml.Decoder
	shared    []string
	dateStyle []bool
}

const xlsxMaxSharedStrings = 2_000_000
const (
	xlsxMaxWorkbookPartBytes     = 4 << 20
	xlsxMaxStylesPartBytes       = 8 << 20
	xlsxMaxSharedStringsBytes    = 16 << 20
	xlsxMaxSharedStringCellBytes = 1 << 20
	xlsxMaxWorksheetBytes        = 256 << 20
)

var cellRefRe = regexp.MustCompile(`^([A-Z]+)`)

// xlsxFile is an opened workbook: its worksheets in workbook order, with the shared-string
// table and the date styles loaded ONCE and shared by every sheet opened from it.
type xlsxFile struct {
	files     map[string]*zip.File
	sheets    []xlsxSheetRef
	shared    []string
	dateStyle []bool
}

// xlsxSheetRef is one worksheet: its tab name and the zip part that holds it.
type xlsxSheetRef struct {
	Name string
	path string
}

// openXLSXFile opens the workbook held in data and lists its worksheets.
func openXLSXFile(data []byte) (*xlsxFile, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("not a workbook")
	}
	wb := &xlsxFile{files: map[string]*zip.File{}}
	for _, f := range zr.File {
		wb.files[f.Name] = f
	}
	sheets, err := xlsxSheetPaths(wb.files)
	if err != nil {
		return nil, err
	}
	wb.sheets = sheets
	if f, ok := wb.files["xl/sharedStrings.xml"]; ok {
		shared, err := loadSharedStrings(f)
		if err != nil {
			return nil, err
		}
		wb.shared = shared
	}
	if f, ok := wb.files["xl/styles.xml"]; ok {
		styles, err := loadStyles(f)
		if err != nil {
			return nil, err
		}
		wb.dateStyle = styles
	}
	return wb, nil
}

// Sheets is the worksheet names in workbook order.
func (wb *xlsxFile) Sheets() []string {
	out := make([]string, len(wb.sheets))
	for i, s := range wb.sheets {
		out[i] = s.Name
	}
	return out
}

// Open positions a row stream on worksheet i.
func (wb *xlsxFile) Open(i int) (*xlsxWorkbook, error) {
	if i < 0 || i >= len(wb.sheets) {
		return nil, errors.New("worksheet missing")
	}
	f, ok := wb.files[wb.sheets[i].path]
	if !ok {
		return nil, errors.New("worksheet missing")
	}
	if f.UncompressedSize64 > xlsxMaxWorksheetBytes {
		return nil, errors.New("worksheet too large")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	return &xlsxWorkbook{sheetXML: rc, dec: xml.NewDecoder(rc), shared: wb.shared, dateStyle: wb.dateStyle}, nil
}

// openXLSX opens the workbook held in data and positions on the first worksheet.
func openXLSX(data []byte) (*xlsxWorkbook, error) {
	wb, err := openXLSXFile(data)
	if err != nil {
		return nil, err
	}
	if len(wb.sheets) == 0 {
		return nil, errors.New("workbook has no sheet")
	}
	return wb.Open(0)
}

// endOfPart reads a part's end as done and anything else as the malformed workbook it is.
func endOfPart(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func readZipXML(f *zip.File, maxBytes int64, visit func(dec *xml.Decoder) error) error {
	if maxBytes > 0 && f.UncompressedSize64 > uint64(maxBytes) {
		return errors.New("workbook part too large")
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	r := io.Reader(rc)
	if maxBytes > 0 {
		r = io.LimitReader(rc, maxBytes+1)
	}
	counting := &countingReader{r: r, max: maxBytes}
	if err := visit(xml.NewDecoder(counting)); err != nil {
		return err
	}
	return counting.err()
}

type countingReader struct {
	r    io.Reader
	max  int64
	read int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.read += int64(n)
	if r.max > 0 && r.read > r.max {
		return n, errors.New("workbook part too large")
	}
	return n, err
}

func (r *countingReader) err() error {
	if r.max > 0 && r.read > r.max {
		return errors.New("workbook part too large")
	}
	return nil
}

// xlsxSheetPaths resolves workbook.xml's <sheet> list, in workbook order, through the
// workbook rels to each worksheet's part.
func xlsxSheetPaths(files map[string]*zip.File) ([]xlsxSheetRef, error) {
	wbFile, ok := files["xl/workbook.xml"]
	if !ok {
		return nil, errors.New("workbook.xml missing")
	}
	type sheetEntry struct{ name, rel string }
	var entries []sheetEntry
	err := readZipXML(wbFile, xlsxMaxWorkbookPartBytes, func(dec *xml.Decoder) error {
		for {
			tok, err := dec.Token()
			if err != nil {
				return endOfPart(err)
			}
			if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "sheet" {
				e := sheetEntry{}
				for _, a := range se.Attr {
					switch a.Name.Local {
					case "id":
						e.rel = a.Value
					case "name":
						e.name = a.Value
					}
				}
				entries = append(entries, e)
			}
		}
	})
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, errors.New("workbook has no sheet")
	}
	targets := map[string]string{}
	if relFile, ok := files["xl/_rels/workbook.xml.rels"]; ok {
		if err := readZipXML(relFile, xlsxMaxWorkbookPartBytes, func(dec *xml.Decoder) error {
			for {
				tok, err := dec.Token()
				if err != nil {
					return endOfPart(err)
				}
				if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "Relationship" {
					id, t := "", ""
					for _, a := range se.Attr {
						switch a.Name.Local {
						case "Id":
							id = a.Value
						case "Target":
							t = a.Value
						}
					}
					targets[id] = t
				}
			}
		}); err != nil {
			return nil, err
		}
	}
	out := make([]xlsxSheetRef, 0, len(entries))
	for i, e := range entries {
		target := targets[e.rel]
		if target == "" {
			target = "worksheets/sheet" + strconv.Itoa(i+1) + ".xml"
		}
		if strings.HasPrefix(target, "/") {
			target = strings.TrimPrefix(target, "/")
		} else {
			target = path.Join("xl", target)
		}
		out = append(out, xlsxSheetRef{Name: e.name, path: target})
	}
	return out, nil
}

func loadSharedStrings(f *zip.File) ([]string, error) {
	var shared []string
	err := readZipXML(f, xlsxMaxSharedStringsBytes, func(dec *xml.Decoder) error {
		var (
			cur        strings.Builder
			inSI       bool
			inT        bool
			count      int
			totalBytes int64
		)
		for {
			tok, err := dec.Token()
			if err != nil {
				return endOfPart(err)
			}
			switch t := tok.(type) {
			case xml.StartElement:
				switch t.Name.Local {
				case "si":
					inSI, cur = true, strings.Builder{}
				case "t":
					inT = inSI
				}
			case xml.CharData:
				if inT {
					totalBytes += int64(len(t))
					if totalBytes > xlsxMaxSharedStringsBytes || cur.Len()+len(t) > xlsxMaxSharedStringCellBytes {
						return errors.New("shared strings too large")
					}
					cur.Write(t)
				}
			case xml.EndElement:
				switch t.Name.Local {
				case "t":
					inT = false
				case "si":
					if count >= xlsxMaxSharedStrings {
						return errors.New("too many strings")
					}
					shared = append(shared, cur.String())
					count++
					inSI = false
				}
			}
		}
	})
	return shared, err
}

// loadStyles marks which cell styles (cellXfs, by index) carry a date number format.
func loadStyles(f *zip.File) ([]bool, error) {
	custom := map[string]bool{}
	var dateStyle []bool
	err := readZipXML(f, xlsxMaxStylesPartBytes, func(dec *xml.Decoder) error {
		inCellXfs := false
		for {
			tok, err := dec.Token()
			if err != nil {
				return endOfPart(err)
			}
			switch t := tok.(type) {
			case xml.StartElement:
				switch t.Name.Local {
				case "numFmt":
					id, code := "", ""
					for _, a := range t.Attr {
						switch a.Name.Local {
						case "numFmtId":
							id = a.Value
						case "formatCode":
							code = a.Value
						}
					}
					custom[id] = xlsxFormatIsDate(code)
				case "cellXfs":
					inCellXfs = true
				case "xf":
					if !inCellXfs {
						continue
					}
					isDate := false
					for _, a := range t.Attr {
						if a.Name.Local == "numFmtId" {
							isDate = xlsxBuiltinDate(a.Value) || custom[a.Value]
						}
					}
					dateStyle = append(dateStyle, isDate)
				}
			case xml.EndElement:
				if t.Name.Local == "cellXfs" {
					inCellXfs = false
				}
			}
		}
	})
	return dateStyle, err
}

func xlsxBuiltinDate(id string) bool {
	n, err := strconv.Atoi(id)
	if err != nil {
		return false
	}
	return (n >= 14 && n <= 22) || (n >= 45 && n <= 47)
}

func xlsxFormatIsDate(code string) bool {
	// Strip quoted literals and colour tags, then look for a day/month/year token.
	c := regexp.MustCompile(`"[^"]*"|\[[^\]]*\]`).ReplaceAllString(strings.ToLower(code), "")
	return strings.ContainsAny(c, "ymd") && !strings.Contains(c, "general")
}

// Next returns the next row's cells, padded to the last filled column; io.EOF at the end.
func (wb *xlsxWorkbook) Next() ([]string, error) {
	var (
		cells  []string
		inRow  bool
		colIdx = -1
		cellT  string
		cellS  string
		inV    bool
		inIS   bool
		value  strings.Builder
		isText bool
	)
	for {
		tok, err := wb.dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if inRow {
					return cells, nil
				}
				return nil, io.EOF
			}
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				inRow, cells = true, cells[:0]
			case "c":
				colIdx, cellT, cellS, isText = -1, "", "", false
				value.Reset()
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "r":
						colIdx = xlsxColumnIndex(a.Value)
					case "t":
						cellT = a.Value
					case "s":
						cellS = a.Value
					}
				}
				if colIdx < 0 {
					colIdx = len(cells)
				}
			case "v":
				inV = true
			case "is":
				inIS = true
			case "t":
				isText = inIS
			}
		case xml.CharData:
			if inV || isText {
				value.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inV = false
			case "t":
				isText = false
			case "is":
				inIS = false
			case "c":
				if colIdx >= 0 && colIdx < 16384 {
					for len(cells) <= colIdx {
						cells = append(cells, "")
					}
					cells[colIdx] = wb.cellValue(cellT, cellS, value.String())
				}
			case "row":
				return cells, nil
			}
		}
	}
}

func (wb *xlsxWorkbook) cellValue(t, s, raw string) string {
	switch t {
	case "s":
		i, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || i < 0 || i >= len(wb.shared) {
			return ""
		}
		return wb.shared[i]
	case "b":
		if strings.TrimSpace(raw) == "1" {
			return "true"
		}
		return "false"
	case "inlineStr", "str":
		return raw
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if si, err := strconv.Atoi(s); err == nil && si >= 0 && si < len(wb.dateStyle) && wb.dateStyle[si] {
		if serial, err := strconv.ParseFloat(raw, 64); err == nil && serial > 0 && serial < 3_000_000 {
			return xlsxSerialDate(serial)
		}
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil && (strings.ContainsAny(raw, "eE") || strings.HasSuffix(raw, ".0")) {
		// Excel writes 12.0 / 9.01E+14 for what a person typed as 12 / 901007000504274.
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return raw
}

// xlsxSerialDate renders an Excel serial day (1900 system) as YYYY-MM-DD.
func xlsxSerialDate(serial float64) string {
	days := math.Floor(serial)
	if days >= 61 {
		days-- // Excel's phantom 1900-02-29
	}
	return time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(days)).Format("2006-01-02")
}

func xlsxColumnIndex(ref string) int {
	m := cellRefRe.FindString(strings.ToUpper(ref))
	if m == "" {
		return -1
	}
	n := 0
	for _, r := range m {
		n = n*26 + int(r-'A') + 1
	}
	return n - 1
}

// Close releases the worksheet stream.
func (wb *xlsxWorkbook) Close() error {
	if wb.sheetXML != nil {
		return wb.sheetXML.Close()
	}
	return nil
}
