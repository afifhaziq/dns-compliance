// Package blockexport flattens the app's case/case-letter data into the
// legacy CRD/CMOD Excel column layouts and writes them as real .xlsx
// workbooks — the export half of internal/blockimport's import.
package blockexport

import (
	"io"
	"strings"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/xuri/excelize/v2"
)

const exportDateLayout = "2006-01-02"

func formatDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(exportDateLayout)
}

func titleCaseFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

type CRDRow struct {
	Tahun            int
	AlamatLamanWeb   string
	ButiranKesalahan string
	Agensi           string
	TarikhBlocked    string
	NoRujukanNMD     string
	Kategori         string
	Elemen           string
	SubElemen        string
	Status           string
	TarikhUplift     string
	NoRujukanNMSMD   string
	Remarks          string
	DotMY            string
	CaseID           uint
	Department       string
	DueDate          string
}

// caseLetterInfo holds the earliest Notice and earliest Notice (Uplift)
// letter for one case, plus its department name.
type caseLetterInfo struct {
	notice     *db.CaseLetterEntry
	uplift     *db.CaseLetterEntry
	department string
}

func earlierLetter(cur, candidate *db.CaseLetterEntry) *db.CaseLetterEntry {
	if cur == nil {
		return candidate
	}
	if candidate.LetterDate == nil {
		return cur
	}
	if cur.LetterDate == nil || candidate.LetterDate.Before(*cur.LetterDate) {
		return candidate
	}
	return cur
}

func indexCRDLettersByCase(letters []db.CaseLetterEntry) map[uint]*caseLetterInfo {
	byCase := make(map[uint]*caseLetterInfo)
	for i := range letters {
		l := &letters[i]
		info, ok := byCase[l.CaseID]
		if !ok {
			info = &caseLetterInfo{}
			byCase[l.CaseID] = info
		}
		if info.department == "" {
			info.department = l.DepartmentName
		}
		switch l.Type {
		case "Notice":
			info.notice = earlierLetter(info.notice, l)
		case "Notice (Uplift)":
			info.uplift = earlierLetter(info.uplift, l)
		}
	}
	return byCase
}

func FlattenCRDRows(cases []db.CaseSummary, letters []db.CaseLetterEntry) []CRDRow {
	infoByCase := indexCRDLettersByCase(letters)

	var out []CRDRow
	for _, c := range cases {
		info := infoByCase[c.ID]
		var notice, uplift *db.CaseLetterEntry
		department := ""
		if info != nil {
			notice, uplift, department = info.notice, info.uplift, info.department
		}

		year := c.CreatedAt.Year()
		if c.RequestedAt != nil {
			year = c.RequestedAt.Year()
		}
		if notice != nil && notice.LetterDate != nil {
			year = notice.LetterDate.Year()
		}

		blocked, refExternal, refInternal, remarks := "", "", "", ""
		if notice != nil {
			blocked = formatDate(notice.LetterDate)
			refExternal = notice.ReferenceNumberExternal
			refInternal = notice.ReferenceNumberInternal
			remarks = notice.Remarks
		}
		upliftDate := ""
		if uplift != nil {
			upliftDate = formatDate(uplift.LetterDate)
		}

		for _, d := range c.Domains {
			my := "No"
			if strings.HasSuffix(d.URL, ".my") {
				my = "Yes"
			}
			base := CRDRow{
				Tahun: year, AlamatLamanWeb: d.URL, Agensi: c.AgencyName,
				TarikhBlocked: blocked, NoRujukanNMD: refExternal,
				Status: titleCaseFirst(d.Status), TarikhUplift: upliftDate,
				NoRujukanNMSMD: refInternal, Remarks: remarks, DotMY: my,
				CaseID: c.ID, Department: department, DueDate: formatDate(c.DueDate),
			}
			if len(d.Offences) == 0 {
				out = append(out, base)
				continue
			}
			for _, off := range d.Offences {
				row := base
				row.ButiranKesalahan = off.Citation
				row.Kategori = off.Category
				row.Elemen = off.Element
				row.SubElemen = off.SubElement
				out = append(out, row)
			}
		}
	}
	return out
}

var crdHeaders = []string{
	"Tahun", "Alamat Laman Web", "Butiran Kesalahan", "Agensi",
	"Tarikh Maklum IASP (Blocked)", "No. Rujukan NMD", "Kategori", "Elemen",
	"Sub-Elemen", "Status", "Tarikh Maklum ISP (Uplift)", "No. Rujukan NMSMD",
	"Remarks", ".my", "Case ID", "Department", "Due Date",
}

// crdDateCols holds the 0-indexed column positions (matching crdHeaders)
// that hold dates and must be written as real Excel date values.
var crdDateCols = map[int]bool{4: true, 10: true, 16: true} // TarikhBlocked, TarikhUplift, DueDate

func WriteCRDWorkbook(rows []CRDRow, w io.Writer) error {
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Sheet1"

	if err := applyHeaderStyle(f, sheet, len(crdHeaders)); err != nil {
		return err
	}
	dateStyle, err := dateCellStyle(f)
	if err != nil {
		return err
	}

	for i, h := range crdHeaders {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return err
		}
		if err := f.SetCellValue(sheet, cell, h); err != nil {
			return err
		}
	}
	for r, row := range rows {
		vals := []interface{}{
			row.Tahun, row.AlamatLamanWeb, row.ButiranKesalahan, row.Agensi,
			row.TarikhBlocked, row.NoRujukanNMD, row.Kategori, row.Elemen,
			row.SubElemen, row.Status, row.TarikhUplift, row.NoRujukanNMSMD,
			row.Remarks, row.DotMY, row.CaseID, row.Department, row.DueDate,
		}
		for c, v := range vals {
			cell, err := excelize.CoordinatesToCellName(c+1, r+2)
			if err != nil {
				return err
			}
			if crdDateCols[c] {
				if err := setDateCell(f, sheet, cell, v.(string), dateStyle); err != nil {
					return err
				}
				continue
			}
			if err := f.SetCellValue(sheet, cell, v); err != nil {
				return err
			}
		}
	}
	return f.Write(w)
}
