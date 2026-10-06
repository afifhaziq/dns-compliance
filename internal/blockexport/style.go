package blockexport

import (
	"fmt"
	"time"

	"github.com/xuri/excelize/v2"
)

const dateNumFmt = "yyyy-mm-dd"

// applyHeaderStyle bolds row 1 across numCols columns and freezes it so it
// stays visible while scrolling — shared by the CRD and CMOD workbooks.
func applyHeaderStyle(f *excelize.File, sheet string, numCols int) error {
	style, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return err
	}
	start, err := excelize.CoordinatesToCellName(1, 1)
	if err != nil {
		return err
	}
	end, err := excelize.CoordinatesToCellName(numCols, 1)
	if err != nil {
		return err
	}
	if err := f.SetCellStyle(sheet, start, end, style); err != nil {
		return err
	}
	return f.SetPanes(sheet, &excelize.Panes{
		Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft",
	})
}

// addTable turns a sheet's header row + dataRows rows into an Excel table
// (filter buttons, banded rows). Excel rejects a header-only table, so an
// empty sheet gets one blank data row.
func addTable(f *excelize.File, sheet string, n, numCols, dataRows int) error {
	end, err := excelize.CoordinatesToCellName(numCols, max(dataRows, 1)+1)
	if err != nil {
		return err
	}
	return f.AddTable(sheet, &excelize.Table{
		Range:     "A1:" + end,
		Name:      fmt.Sprintf("Table%d", n),
		StyleName: "TableStyleMedium2",
	})
}

// dateCellStyle creates (once per workbook) a style that formats a cell as
// yyyy-mm-dd.
func dateCellStyle(f *excelize.File) (int, error) {
	numFmt := dateNumFmt
	return f.NewStyle(&excelize.Style{CustomNumFmt: &numFmt})
}

// setDateCell writes a date string (formatted via exportDateLayout, or ""
// for blank) into cell as a real Excel date value styled as yyyy-mm-dd, so
// spreadsheet apps treat it as a sortable/filterable date rather than text.
func setDateCell(f *excelize.File, sheet, cell, dateStr string, dateStyle int) error {
	if dateStr == "" {
		return f.SetCellValue(sheet, cell, "")
	}
	t, err := time.Parse(exportDateLayout, dateStr)
	if err != nil {
		// Shouldn't happen — dateStr always comes from formatDate using
		// this same layout — but don't lose data if it ever does.
		return f.SetCellValue(sheet, cell, dateStr)
	}
	if err := f.SetCellValue(sheet, cell, t); err != nil {
		return err
	}
	return f.SetCellStyle(sheet, cell, cell, dateStyle)
}
