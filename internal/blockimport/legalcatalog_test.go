package blockimport

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseInstrumentText(t *testing.T) {
	cases := []struct {
		raw        string
		wantType   string
		wantJur    string
		wantNumber string
		wantYear   *int
		wantTitle  string
	}{
		{"Akta Rumah Judi Terbuka 1953 (Akta 289)", "ACT", "FEDERAL", "289", intPtr(1953), "Akta Rumah Judi Terbuka 1953"},
		{"Kanun Keseksaan", "ACT", "FEDERAL", "", nil, "Kanun Keseksaan"},
		{"Enakmen Jenayah Syariah (Negeri Selangor) 1995", "ENACTMENT", "Selangor", "", intPtr(1995), "Enakmen Jenayah Syariah (Negeri Selangor) 1995"},
		{"Ordinan Kesalahan Jenayah Syariah Sarawak 2001", "ORDINANCE", "Sarawak", "", intPtr(2001), "Ordinan Kesalahan Jenayah Syariah Sarawak 2001"},
		{"Peraturan-peraturan Kawalan Hasil Tembakau 2004", "REGULATION", "FEDERAL", "", intPtr(2004), "Peraturan-peraturan Kawalan Hasil Tembakau 2004"},
		{"Kaedah-Kaedah Pendaftaran Perniagaan 1957", "SUBSIDIARY", "FEDERAL", "", intPtr(1957), "Kaedah-Kaedah Pendaftaran Perniagaan 1957"},
		{"Akta Kesalahan Jenayah Syariah (Wilayah-Wilayah Persekutuan) 1997", "ACT", "FEDERAL", "", intPtr(1997), "Akta Kesalahan Jenayah Syariah (Wilayah-Wilayah Persekutuan) 1997"},
	}
	for _, c := range cases {
		got := parseInstrumentText(c.raw)
		if got.Type != c.wantType || got.Jurisdiction != c.wantJur || got.Number != c.wantNumber || got.ShortTitle != c.wantTitle {
			t.Errorf("parseInstrumentText(%q) = %+v", c.raw, got)
		}
		if (got.Year == nil) != (c.wantYear == nil) || (got.Year != nil && *got.Year != *c.wantYear) {
			t.Errorf("parseInstrumentText(%q).Year = %v, want %v", c.raw, got.Year, c.wantYear)
		}
	}
}

func intPtr(i int) *int { return &i }

func TestLoadCitationClassification(t *testing.T) {
	csv := `raw_citation,row_count,instrument,citation_provision,status,notes
Seksyen 233 Akta Komunikasi dan Multimedia 1998,7532,Akta Komunikasi dan Multimedia 1998,Seksyen 233,confirmed,note
Seksyen 211 dan 233 Akta Komunikasi dan Multimedia 1998,1067,Akta Komunikasi dan Multimedia 1998,Seksyen 211,confirmed,split
Seksyen 211 dan 233 Akta Komunikasi dan Multimedia 1998,1067,Akta Komunikasi dan Multimedia 1998,Seksyen 233,confirmed,split
Some Unresolved Citation,5,Some Act,Seksyen 1,needs_decision,not yet
`
	path := filepath.Join(t.TempDir(), "classification.csv")
	if err := os.WriteFile(path, []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadCitationClassification(path)
	if err != nil {
		t.Fatalf("LoadCitationClassification: %v", err)
	}
	if len(got["Seksyen 233 Akta Komunikasi dan Multimedia 1998"]) != 1 {
		t.Fatalf("simple citation: got %+v", got["Seksyen 233 Akta Komunikasi dan Multimedia 1998"])
	}
	compound := got["Seksyen 211 dan 233 Akta Komunikasi dan Multimedia 1998"]
	if len(compound) != 2 || compound[0].Provision != "Seksyen 211" || compound[1].Provision != "Seksyen 233" {
		t.Fatalf("compound citation: got %+v", compound)
	}
	if _, ok := got["Some Unresolved Citation"]; ok {
		t.Fatalf("needs_decision row should be excluded, got an entry")
	}
}
