package blockexport

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/afif/dns-tracking/internal/db"
)

// RegisterFirstYear is where the blocking register (and the MCMC stats
// workbook it mirrors) starts counting.
const RegisterFirstYear = 2022

var malayMonths = [...]string{"Januari", "Februari", "Mac", "April", "Mei", "Jun", "Julai", "Ogos", "September", "Oktober", "November", "Disember"}

// registerLine is one (agency, offence) row with its per-year counts.
type registerLine struct {
	agency, offence string
	byYear          map[int]int
	total           int
}

func (l registerLine) count(year int) int {
	if year == 0 {
		return l.total
	}
	return l.byYear[year]
}

// registerLines splits rows (already agency-attributed by db.BlockingStats)
// into MCMC's lines — the five workbook categories in workbook order, even
// at zero, then any stray MCMC offence — and every other agency's lines,
// agencies by total then offences by total. Also returns the years present,
// ascending.
func registerLines(rows []db.BlockingStatRow) (mcmc, others []registerLine, years []int) {
	byKey := map[[2]string]*registerLine{}
	yearSet := map[int]bool{}
	for _, r := range rows {
		if r.Year < RegisterFirstYear {
			continue
		}
		yearSet[r.Year] = true
		k := [2]string{r.Agency, strings.ToLower(r.Offence)}
		l := byKey[k]
		if l == nil {
			l = &registerLine{agency: r.Agency, offence: r.Offence, byYear: map[int]int{}}
			byKey[k] = l
		}
		l.byYear[r.Year] += r.Count
		l.total += r.Count
	}
	for y := range yearSet {
		years = append(years, y)
	}
	sort.Ints(years)

	for _, c := range db.MCMCCategories {
		k := [2]string{"MCMC", strings.ToLower(c)}
		if l := byKey[k]; l != nil {
			mcmc = append(mcmc, *l)
			delete(byKey, k)
		} else {
			mcmc = append(mcmc, registerLine{agency: "MCMC", offence: c, byYear: map[int]int{}})
		}
	}
	agencyTotal := map[string]int{}
	var stray []registerLine
	for _, l := range byKey {
		if l.agency == "MCMC" {
			stray = append(stray, *l)
		} else {
			others = append(others, *l)
			agencyTotal[l.agency] += l.total
		}
	}
	byTotal := func(ls []registerLine) func(i, j int) bool {
		return func(i, j int) bool {
			a, b := ls[i], ls[j]
			if a.agency != b.agency {
				if agencyTotal[a.agency] != agencyTotal[b.agency] {
					return agencyTotal[a.agency] > agencyTotal[b.agency]
				}
				return a.agency < b.agency
			}
			if a.total != b.total {
				return a.total > b.total
			}
			return a.offence < b.offence
		}
	}
	sort.Slice(stray, byTotal(stray))
	sort.Slice(others, byTotal(others))
	return append(mcmc, stray...), others, years
}

type registerStyles struct {
	title, heading, number, header, label, count, pct, totalLabel, totalCount, totalPct int
}

func newRegisterStyles(f *excelize.File) (registerStyles, error) {
	font := func(bold bool) *excelize.Font { return &excelize.Font{Bold: bold, Size: 12} }
	fill := func(c string) excelize.Fill { return excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{c}} }
	border := []excelize.Border{{Type: "left", Style: 1, Color: "000000"}, {Type: "right", Style: 1, Color: "000000"}, {Type: "top", Style: 1, Color: "000000"}, {Type: "bottom", Style: 1, Color: "000000"}}
	center := &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true}
	left := &excelize.Alignment{Horizontal: "left", Vertical: "center", WrapText: true}
	pctFmt := "0.00%"
	specs := []*excelize.Style{
		{Font: font(true), Fill: fill("FFC000"), Alignment: center},
		{Font: font(true), Alignment: left},
		{Font: font(true), Alignment: &excelize.Alignment{Horizontal: "right"}},
		{Font: font(true), Fill: fill("C1E5F5"), Border: border, Alignment: center},
		{Font: font(false), Border: border, Alignment: left},
		{Font: font(false), Border: border, Alignment: center, NumFmt: 3},
		{Font: font(false), Border: border, Alignment: center, CustomNumFmt: &pctFmt},
		{Font: font(true), Fill: fill("FBE3D6"), Border: border, Alignment: left},
		{Font: font(true), Fill: fill("FBE3D6"), Border: border, Alignment: center, NumFmt: 3},
		{Font: font(true), Fill: fill("FBE3D6"), Border: border, Alignment: center, CustomNumFmt: &pctFmt},
	}
	ids := make([]int, len(specs))
	for i, s := range specs {
		id, err := f.NewStyle(s)
		if err != nil {
			return registerStyles{}, err
		}
		ids[i] = id
	}
	return registerStyles{ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], ids[6], ids[7], ids[8], ids[9]}, nil
}

// registerSheet writes one sheet top to bottom; the first error sticks and
// every later call is a no-op, so writers read as a flat layout script.
type registerSheet struct {
	f    *excelize.File
	name string
	st   registerStyles
	row  int
	err  error
}

func cellName(col, row int) string {
	c, _ := excelize.CoordinatesToCellName(col, row)
	return c
}

// set writes v into (col, row) styled, merging across to (col2, row2) when
// that's a different cell.
func (s *registerSheet) set(col, row, col2, row2 int, v any, style int) {
	if s.err != nil {
		return
	}
	a, b := cellName(col, row), cellName(col2, row2)
	if a != b {
		if s.err = s.f.MergeCell(s.name, a, b); s.err != nil {
			return
		}
	}
	if v != nil {
		if s.err = s.f.SetCellValue(s.name, a, v); s.err != nil {
			return
		}
	}
	s.err = s.f.SetCellStyle(s.name, a, b, style)
}

// heading writes a numbered section heading spanning B..lastCol and leaves
// one blank row under it.
func (s *registerSheet) heading(n int, text string, lastCol int) {
	s.set(1, s.row, 1, s.row, n, s.st.number)
	s.set(2, s.row, lastCol, s.row, text, s.st.heading)
	s.row += 2
}

func pctOf(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total)
}

// countSection is an A/B-sheet table: label column(s), Jumlah Sekatan,
// Peratusan, then a Jumlah Keseluruhan row. year 0 = every year.
func (s *registerSheet) countSection(n int, heading string, labels []string, lines []registerLine, year int) {
	nl := len(labels)
	s.heading(n, heading, nl+3)
	for i, h := range append(labels, "Jumlah Sekatan", "Peratusan") {
		s.set(2+i, s.row, 2+i, s.row, h, s.st.header)
	}
	s.row++
	total := 0
	for _, l := range lines {
		total += l.count(year)
	}
	for _, l := range lines {
		vals := []string{l.offence}
		if nl == 2 {
			vals = []string{l.agency, l.offence}
		}
		for i, v := range vals {
			s.set(2+i, s.row, 2+i, s.row, v, s.st.label)
		}
		s.set(2+nl, s.row, 2+nl, s.row, l.count(year), s.st.count)
		s.set(3+nl, s.row, 3+nl, s.row, pctOf(l.count(year), total), s.st.pct)
		s.row++
	}
	s.set(2, s.row, 1+nl, s.row, "Jumlah Keseluruhan", s.st.totalLabel)
	s.set(2+nl, s.row, 2+nl, s.row, total, s.st.totalCount)
	s.set(3+nl, s.row, 3+nl, s.row, pctOf(total, total), s.st.totalPct)
	s.row += 3
}

// yearSection is a C-sheet table: label column(s), a "Tahun" band over one
// column per year, Jumlah, Peratusan (%), then a Jumlah Keseluruhan row.
func (s *registerSheet) yearSection(n int, heading string, labels []string, lines []registerLine, years []int, totalHeader string) {
	nl := len(labels)
	last := 1 + nl + len(years) + 2
	s.heading(n, heading, last)
	top := s.row
	for i, h := range labels {
		s.set(2+i, top, 2+i, top+1, h, s.st.header)
	}
	if len(years) > 0 {
		s.set(2+nl, top, 1+nl+len(years), top, "Tahun", s.st.header)
	}
	for i, y := range years {
		s.set(2+nl+i, top+1, 2+nl+i, top+1, y, s.st.header)
	}
	s.set(last-1, top, last-1, top+1, totalHeader, s.st.header)
	s.set(last, top, last, top+1, "Peratusan (%)", s.st.header)
	s.row = top + 2

	total := 0
	yearTotal := make([]int, len(years))
	for _, l := range lines {
		total += l.total
		for i, y := range years {
			yearTotal[i] += l.byYear[y]
		}
	}
	for _, l := range lines {
		vals := []string{l.agency}
		if nl == 2 {
			vals = []string{l.agency, l.offence}
		}
		for i, v := range vals {
			s.set(2+i, s.row, 2+i, s.row, v, s.st.label)
		}
		for i, y := range years {
			s.set(2+nl+i, s.row, 2+nl+i, s.row, l.byYear[y], s.st.count)
		}
		s.set(last-1, s.row, last-1, s.row, l.total, s.st.count)
		s.set(last, s.row, last, s.row, pctOf(l.total, total), s.st.pct)
		s.row++
	}
	s.set(2, s.row, 1+nl, s.row, "Jumlah Keseluruhan", s.st.totalLabel)
	for i, v := range yearTotal {
		s.set(2+nl+i, s.row, 2+nl+i, s.row, v, s.st.totalCount)
	}
	s.set(last-1, s.row, last-1, s.row, total, s.st.totalCount)
	s.set(last, s.row, last, s.row, pctOf(total, total), s.st.totalPct)
	s.row += 3
}

// WriteBlockingRegisterWorkbook writes the blocking register in the layout
// of the MCMC stats workbook ("Jumlah Sekatan Laman Sesawang"): A (MCMC's
// blocks by offence, all years then each year newest first), B (other
// agencies' blocks by agency + offence, same sections), C (comparison:
// MCMC vs other agencies by year, then every agency + offence by year).
// Its narrative "D. Sample" sheet isn't data the system holds, so it's left
// out. rows come from db.BlockingStats, asOf is the "Setakat" date.
func WriteBlockingRegisterWorkbook(rows []db.BlockingStatRow, asOf time.Time, w io.Writer) error {
	f := excelize.NewFile()
	defer f.Close()
	st, err := newRegisterStyles(f)
	if err != nil {
		return err
	}
	mcmc, others, years := registerLines(rows)
	span := ""
	if len(years) > 0 {
		span = fmt.Sprintf(" (%d - %d)", years[0], years[len(years)-1])
	}
	d := asOf.In(myt)
	setakat := fmt.Sprintf("Setakat %d %s %d", d.Day(), malayMonths[d.Month()-1], d.Year())

	const (
		sheetA = "A. Jumlah Sekatan MCMC"
		sheetB = "B. Jumlah Sekatan Agensi"
		sheetC = "C. Perbandingan"
	)
	if err := f.SetSheetName("Sheet1", sheetA); err != nil {
		return err
	}
	for _, n := range []string{sheetB, sheetC} {
		if _, err := f.NewSheet(n); err != nil {
			return err
		}
	}

	a := &registerSheet{f: f, name: sheetA, st: st, row: 4}
	a.set(2, 2, 4, 2, "A. JUMLAH SEKATAN LAMAN SESAWANG OLEH MCMC ("+setakat+")", st.title)
	a.countSection(1, "Jumlah Keseluruhan Sekatan Laman Sesawang oleh MCMC"+span, []string{"Elemen/Kesalahan"}, mcmc, 0)
	for i := len(years) - 1; i >= 0; i-- {
		a.countSection(len(years)-i+1, fmt.Sprintf("Jumlah Sekatan Laman Sesawang oleh MCMC bagi tahun %d", years[i]), []string{"Elemen/Kesalahan"}, mcmc, years[i])
	}

	b := &registerSheet{f: f, name: sheetB, st: st, row: 4}
	b.set(2, 2, 5, 2, "B. JUMLAH SEKATAN LAMAN SESAWANG OLEH AGENSI YANG LAIN ("+setakat+")", st.title)
	b.countSection(1, "Jumlah Keseluruhan Sekatan Laman Sesawang oleh Agensi Lain"+span, []string{"Agensi", "Elemen/Kesalahan"}, others, 0)
	for i := len(years) - 1; i >= 0; i-- {
		b.countSection(len(years)-i+1, fmt.Sprintf("Jumlah Sekatan Laman Sesawang oleh Agensi Lain bagi tahun %d", years[i]), []string{"Agensi", "Elemen/Kesalahan"}, others, years[i])
	}

	sum := func(name string, ls []registerLine) registerLine {
		out := registerLine{agency: name, byYear: map[int]int{}}
		for _, l := range ls {
			for y, n := range l.byYear {
				out.byYear[y] += n
			}
			out.total += l.total
		}
		return out
	}
	c := &registerSheet{f: f, name: sheetC, st: st, row: 4}
	c.set(2, 2, 3+len(years)+2, 2, "C. PERBANDINGAN MENGIKUT BIDANG KUASA DAN KESALAHAN ("+setakat+")", st.title)
	c.yearSection(1, "Perbandingan Keseluruhan Sekatan Laman Sesawang mengikut Bidang Kuasa"+span, []string{"Bidang Kuasa"},
		[]registerLine{sum("MCMC", mcmc), sum("Agensi Lain", others)}, years, "Jumlah")
	c.yearSection(2, "Perbandingan Keseluruhan Sekatan Laman Sesawang mengikut Elemen/Kesalahan"+span, []string{"Agensi", "Elemen/Kesalahan"},
		append(append([]registerLine{}, mcmc...), others...), years, "Jumlah Keseluruhan")

	for _, s := range []*registerSheet{a, b, c} {
		if s.err != nil {
			return s.err
		}
	}
	widths := map[string]map[string]float64{
		sheetA: {"A": 6, "B": 57.5, "C": 24, "D": 24},
		sheetB: {"A": 6, "B": 22.5, "C": 71, "D": 24, "E": 24},
		sheetC: {"A": 6, "B": 22.5, "C": 57.5},
	}
	for sheet, cols := range widths {
		for col, wd := range cols {
			if err := f.SetColWidth(sheet, col, col, wd); err != nil {
				return err
			}
		}
	}
	if last, _ := excelize.ColumnNumberToName(3 + len(years) + 2); len(years) > 0 {
		if err := f.SetColWidth(sheetC, "D", last, 14); err != nil {
			return err
		}
	}
	return f.Write(w)
}
