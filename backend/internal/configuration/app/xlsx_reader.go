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
// the FIRST worksheet in workbook order; shared strings (rich-text runs joined); inline strings;
// booleans; numbers, rendered as written; and dates -- a numeric cell under a date number format
// becomes YYYY-MM-DD, so a date-of-birth typed in Excel arrives as the register expects. Rows
// stream one at a time; only the shared-string table is held (a few MB for a large sheet).

type xlsxWorkbook struct {
	sheetXML  io.ReadCloser
	dec       *xml.Decoder
	shared    []string
	dateStyle []bool
	closer    io.Closer
}

const xlsxMaxSharedStrings = 2_000_000

var cellRefRe = regexp.MustCompile(`^([A-Z]+)`)

// openXLSX opens the workbook held in data and positions on the first worksheet.
func openXLSX(data []byte) (*xlsxWorkbook, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("not a workbook")
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	sheetPath, err := xlsxFirstSheetPath(files)
	if err != nil {
		return nil, err
	}
	wb := &xlsxWorkbook{}
	if f, ok := files["xl/sharedStrings.xml"]; ok {
		if err := wb.loadSharedStrings(f); err != nil {
			return nil, err
		}
	}
	if f, ok := files["xl/styles.xml"]; ok {
		if err := wb.loadStyles(f); err != nil {
			return nil, err
		}
	}
	f, ok := files[sheetPath]
	if !ok {
		return nil, errors.New("worksheet missing")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	wb.sheetXML = rc
	wb.dec = xml.NewDecoder(rc)
	return wb, nil
}

// endOfPart reads a part's end as done and anything else as the malformed workbook it is.
func endOfPart(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func readZipXML(f *zip.File, visit func(dec *xml.Decoder) error) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return visit(xml.NewDecoder(rc))
}

// xlsxFirstSheetPath resolves workbook.xml's first <sheet> through the workbook rels.
func xlsxFirstSheetPath(files map[string]*zip.File) (string, error) {
	wbFile, ok := files["xl/workbook.xml"]
	if !ok {
		return "", errors.New("workbook.xml missing")
	}
	firstRel := ""
	err := readZipXML(wbFile, func(dec *xml.Decoder) error {
		for {
			tok, err := dec.Token()
			if err != nil {
				return endOfPart(err)
			}
			if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "sheet" {
				for _, a := range se.Attr {
					if a.Name.Local == "id" {
						firstRel = a.Value
					}
				}
				return nil
			}
		}
	})
	if err != nil {
		return "", err
	}
	if firstRel == "" {
		return "", errors.New("workbook has no sheet")
	}
	target := ""
	if relFile, ok := files["xl/_rels/workbook.xml.rels"]; ok {
		_ = readZipXML(relFile, func(dec *xml.Decoder) error {
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
					if id == firstRel {
						target = t
						return nil
					}
				}
			}
		})
	}
	if target == "" {
		target = "worksheets/sheet1.xml"
	}
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(target, "/"), nil
	}
	return path.Join("xl", target), nil
}

func (wb *xlsxWorkbook) loadSharedStrings(f *zip.File) error {
	return readZipXML(f, func(dec *xml.Decoder) error {
		var (
			cur   strings.Builder
			inSI  bool
			inT   bool
			count int
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
					wb.shared = append(wb.shared, cur.String())
					count++
					inSI = false
				}
			}
		}
	})
}

// loadStyles marks which cell styles (cellXfs, by index) carry a date number format.
func (wb *xlsxWorkbook) loadStyles(f *zip.File) error {
	custom := map[string]bool{}
	return readZipXML(f, func(dec *xml.Decoder) error {
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
					wb.dateStyle = append(wb.dateStyle, isDate)
				}
			case xml.EndElement:
				if t.Name.Local == "cellXfs" {
					inCellXfs = false
				}
			}
		}
	})
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
