package legacy_import

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var requiredWorkbookColumns = []string{
	"Farm",
	"Old ID",
	"Old ID Suffix",
	"RFID",
	"Age",
	"Gender",
	"Breed",
	"Tag",
	"Shed",
	"Partition",
}

func ParseXLSX(data []byte) ([]WorkbookRow, error) {
	if len(data) == 0 {
		return nil, errors.New("workbook is empty")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("input workbook is not a valid xlsx file")
	}

	sharedStrings, err := readSharedStrings(reader)
	if err != nil {
		return nil, err
	}
	sheetPath, err := firstSheetPath(reader)
	if err != nil {
		return nil, err
	}
	sheetBytes, err := readZipFile(reader, sheetPath)
	if err != nil {
		return nil, fmt.Errorf("read first worksheet: %w", err)
	}
	return parseWorksheet(sheetBytes, sharedStrings)
}

func readSharedStrings(reader *zip.Reader) ([]string, error) {
	data, err := readZipFile(reader, "xl/sharedStrings.xml")
	if err != nil {
		if errors.Is(err, errZipFileNotFound) {
			return nil, nil
		}
		return nil, err
	}

	decoder := xml.NewDecoder(bytes.NewReader(data))
	var stringsOut []string
	var inSI bool
	var builder strings.Builder
	for {
		tok, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse shared strings: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "si" {
				inSI = true
				builder.Reset()
			}
			if inSI && t.Name.Local == "t" {
				var value string
				if err := decoder.DecodeElement(&value, &t); err != nil {
					return nil, fmt.Errorf("parse shared string text: %w", err)
				}
				builder.WriteString(value)
			}
		case xml.EndElement:
			if t.Name.Local == "si" {
				inSI = false
				stringsOut = append(stringsOut, builder.String())
			}
		}
	}
	return stringsOut, nil
}

func firstSheetPath(reader *zip.Reader) (string, error) {
	workbook, err := readZipFile(reader, "xl/workbook.xml")
	if err != nil {
		return "xl/worksheets/sheet1.xml", nil
	}
	var wb struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(workbook, &wb); err != nil || len(wb.Sheets) == 0 || wb.Sheets[0].RID == "" {
		return "xl/worksheets/sheet1.xml", nil
	}

	relsData, err := readZipFile(reader, "xl/_rels/workbook.xml.rels")
	if err != nil {
		return "xl/worksheets/sheet1.xml", nil
	}
	var rels struct {
		Relationships []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(relsData, &rels); err != nil {
		return "xl/worksheets/sheet1.xml", nil
	}
	for _, rel := range rels.Relationships {
		if rel.ID == wb.Sheets[0].RID && rel.Target != "" {
			target := strings.TrimPrefix(rel.Target, "/")
			if strings.HasPrefix(target, "xl/") {
				return path.Clean(target), nil
			}
			return path.Clean("xl/" + target), nil
		}
	}
	return "xl/worksheets/sheet1.xml", nil
}

type inlineStringXML struct {
	Text []string `xml:"t"`
}

type cellXML struct {
	Ref    string          `xml:"r,attr"`
	Type   string          `xml:"t,attr"`
	Value  string          `xml:"v"`
	Inline inlineStringXML `xml:"is"`
}

type rowXML struct {
	Number int       `xml:"r,attr"`
	Cells  []cellXML `xml:"c"`
}

type worksheetXML struct {
	Rows []rowXML `xml:"sheetData>row"`
}

func parseWorksheet(data []byte, sharedStrings []string) ([]WorkbookRow, error) {
	var ws worksheetXML
	if err := xml.Unmarshal(data, &ws); err != nil {
		return nil, fmt.Errorf("parse worksheet: %w", err)
	}
	if len(ws.Rows) == 0 {
		return nil, errors.New("workbook first worksheet has no rows")
	}

	header := map[int]string{}
	for _, cell := range ws.Rows[0].Cells {
		name := strings.TrimSpace(cellValue(cell, sharedStrings))
		if name == "" {
			continue
		}
		header[columnIndex(cell.Ref)] = name
	}
	if err := validateHeaders(header); err != nil {
		return nil, err
	}

	rows := make([]WorkbookRow, 0, len(ws.Rows)-1)
	for _, row := range ws.Rows[1:] {
		rowNumber := row.Number
		if rowNumber == 0 {
			rowNumber = len(rows) + 2
		}
		raw := make(map[string]string, len(requiredWorkbookColumns))
		for _, name := range requiredWorkbookColumns {
			raw[name] = ""
		}
		nonEmpty := false
		for _, cell := range row.Cells {
			name := header[columnIndex(cell.Ref)]
			if name == "" {
				continue
			}
			value := cellValue(cell, sharedStrings)
			raw[name] = value
			if strings.TrimSpace(value) != "" {
				nonEmpty = true
			}
		}
		if nonEmpty {
			rows = append(rows, WorkbookRow{RowNumber: rowNumber, Raw: raw})
		}
	}
	return rows, nil
}

func cellValue(cell cellXML, sharedStrings []string) string {
	switch cell.Type {
	case "s":
		idx, err := strconv.Atoi(strings.TrimSpace(cell.Value))
		if err != nil || idx < 0 || idx >= len(sharedStrings) {
			return strings.TrimSpace(cell.Value)
		}
		return sharedStrings[idx]
	case "inlineStr":
		return strings.Join(cell.Inline.Text, "")
	default:
		return cell.Value
	}
}

func validateHeaders(header map[int]string) error {
	seen := map[string]bool{}
	for _, name := range header {
		seen[strings.ToLower(strings.TrimSpace(name))] = true
	}
	var missing []string
	for _, required := range requiredWorkbookColumns {
		if !seen[strings.ToLower(required)] {
			missing = append(missing, required)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("workbook missing required columns: %s", strings.Join(missing, ", "))
	}
	return nil
}

var cellRefPattern = regexp.MustCompile(`^[A-Z]+`)

func columnIndex(ref string) int {
	letters := cellRefPattern.FindString(strings.ToUpper(ref))
	if letters == "" {
		return 0
	}
	idx := 0
	for _, r := range letters {
		idx = idx*26 + int(r-'A'+1)
	}
	return idx
}

var errZipFileNotFound = errors.New("zip file not found")

func readZipFile(reader *zip.Reader, name string) ([]byte, error) {
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return nil, errZipFileNotFound
}
