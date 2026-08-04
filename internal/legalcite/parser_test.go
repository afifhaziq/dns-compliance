package legalcite_test

import (
	"testing"

	"github.com/afif/dns-tracking/internal/legalcite"
)

func iptr(n int) *int { return &n }

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want legalcite.Parsed
		conf string
	}{
		{
			name: "seksyen subsection paragraph",
			raw:  "Seksyen 233(1)(a)",
			want: legalcite.Parsed{ProvisionNum: iptr(233), SubProvision: iptr(1), Paragraph: "a"},
			conf: legalcite.ConfidenceOK,
		},
		{
			name: "bahagian only",
			raw:  "Bahagian IX",
			want: legalcite.Parsed{Part: iptr(9)},
			conf: legalcite.ConfidenceOK,
		},
		{
			name: "perkara with subsection suffix — constitution style, suffix captured regardless of instrument type",
			raw:  "Perkara 121(1A)",
			want: legalcite.Parsed{ProvisionNum: iptr(121), SubProvision: iptr(1), SubProvisionSuffix: "A"},
			conf: legalcite.ConfidenceOK,
		},
		{
			name: "seksyen with amendment suffix",
			raw:  "Seksyen 4A",
			want: legalcite.Parsed{ProvisionNum: iptr(4), ProvisionSuffix: "A"},
			conf: legalcite.ConfidenceOK,
		},
		{
			name: "no label word — label is optional",
			raw:  "121(1A)",
			want: legalcite.Parsed{ProvisionNum: iptr(121), SubProvision: iptr(1), SubProvisionSuffix: "A"},
			conf: legalcite.ConfidenceOK,
		},
		{
			name: "jadual with senarai — keyword-first Malay order, opposite of English \"First Schedule\"",
			raw:  "Jadual Pertama, Senarai II",
			want: legalcite.Parsed{Schedule: iptr(1), ScheduleList: iptr(2)},
			conf: legalcite.ConfidenceOK,
		},
		{
			name: "roman jumps straight to subparagraph — ambiguous, flagged not guessed",
			raw:  "Seksyen 233(i)",
			want: legalcite.Parsed{ProvisionNum: iptr(233)},
			conf: legalcite.ConfidenceNeedsReview,
		},
		{
			name: "full depth: subsection, paragraph, subparagraph, sub-subparagraph",
			raw:  "Seksyen 5(2)(b)(iii)(C)",
			want: legalcite.Parsed{
				ProvisionNum: iptr(5), SubProvision: iptr(2), Paragraph: "b",
				Subparagraph: "iii", SubSubparagraph: "C",
			},
			conf: legalcite.ConfidenceOK,
		},
		{
			name: "unparseable garbage",
			raw:  "whatever this is not a citation",
			want: legalcite.Parsed{},
			conf: legalcite.ConfidenceNeedsReview,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := legalcite.Parse(tc.raw)
			if got.Confidence != tc.conf {
				t.Fatalf("Confidence = %q, want %q (parsed: %+v)", got.Confidence, tc.conf, got.Parsed)
			}
			if !parsedEqual(got.Parsed, tc.want) {
				t.Fatalf("Parsed = %+v, want %+v", got.Parsed, tc.want)
			}
		})
	}
}

// TestParse_RealWorldMalaySamples uses 10 randomly-sampled "Butiran
// Kesalahan" values pulled from MCMC's actual national blocking-list
// spreadsheet — real analyst input, not the brief's English-shorthand
// examples. Confirms the Malay labels (seksyen/peraturan) parse, and that a
// citation listing multiple provisions in one string ("... dan ...", a
// comma-separated list) is flagged NEEDS_REVIEW rather than silently
// keeping only the first number and dropping the rest.
func TestParse_RealWorldMalaySamples(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want legalcite.Parsed
		conf string
	}{
		{
			name: "seksyen only, no sub-provision",
			raw:  "Seksyen 4 Akta Hasutan 1948",
			want: legalcite.Parsed{ProvisionNum: iptr(4)},
			conf: legalcite.ConfidenceOK,
		},
		{
			name: "seksyen with bare paragraph letter — ambiguous position, flagged not guessed",
			raw:  "Seksyen 17 (d) Akta Makanan 1983",
			want: legalcite.Parsed{ProvisionNum: iptr(17)},
			conf: legalcite.ConfidenceNeedsReview,
		},
		{
			name: "seksyen followed by instrument name with its own parenthetical",
			raw:  "Seksyen 4 Akta Ubat  (Iklan & Jualan) 1956",
			want: legalcite.Parsed{ProvisionNum: iptr(4)},
			conf: legalcite.ConfidenceNeedsReview,
		},
		{
			name: "comma-and-dan joined list of provisions — first number kept, flagged",
			raw:  "Seksyen 7, 9, 10, 11, 13 dan 16 Enakmen Jenayah Syariah (Negeri Selangor) 1997",
			want: legalcite.Parsed{ProvisionNum: iptr(7)},
			conf: legalcite.ConfidenceNeedsReview,
		},
		{
			name: "peraturan label, subsection and paragraph",
			raw:  "Peraturan 5(1)(a) Peraturan Dadah Merbahaya 1952",
			want: legalcite.Parsed{ProvisionNum: iptr(5), SubProvision: iptr(1), Paragraph: "a"},
			conf: legalcite.ConfidenceOK,
		},
		{
			name: "peraturan label, hyphenated instrument name",
			raw:  "Peraturan 7(1)(a) Peraturan-peraturan Kawalan Dadah & Kosmetik 1984",
			want: legalcite.Parsed{ProvisionNum: iptr(7), SubProvision: iptr(1), Paragraph: "a"},
			conf: legalcite.ConfidenceOK,
		},
		{
			name: "dan seksyen — repeated label joining two provisions",
			raw:  "Seksyen 5 dan Seksyen 11 Akta Pemberi Pinjam Wang 1951",
			want: legalcite.Parsed{ProvisionNum: iptr(5)},
			conf: legalcite.ConfidenceNeedsReview,
		},
		{
			name: "seksyen with spaced bare-letter paragraph — same ambiguity as case 2",
			raw:  "Seksyen 12 (c ) Enakmen Jenayah Syariah (Negeri Selangor) 1995",
			want: legalcite.Parsed{ProvisionNum: iptr(12)},
			conf: legalcite.ConfidenceNeedsReview,
		},
		{
			name: "seksyen only, longer instrument name",
			raw:  "Seksyen 4 Akta Rumah Judi Terbuka 1966",
			want: legalcite.Parsed{ProvisionNum: iptr(4)},
			conf: legalcite.ConfidenceOK,
		},
		{
			name: "seksyen only — instrument name's own \"dan\" must not be mistaken for a joined list",
			raw:  "Seksyen 58 Akta Undang Undang Pasaran Modal dan Perkhidmatan 2007",
			want: legalcite.Parsed{ProvisionNum: iptr(58)},
			conf: legalcite.ConfidenceOK,
		},
		{
			name: "ampersand-joined provisions — bug: parser didn't recognize '&' as a list joiner",
			raw:  "Seksyen 292 & Seksyen 372 Kanun Keseksaan",
			want: legalcite.Parsed{ProvisionNum: iptr(292)},
			conf: legalcite.ConfidenceNeedsReview,
		},
		{
			name: "ampersand-joined provisions, peraturan label, no instrument suffix words",
			raw:  "Peraturan 62 & 63 Enakmen Kesalahan Syariah Negeri Melaka 1991",
			want: legalcite.Parsed{ProvisionNum: iptr(62)},
			conf: legalcite.ConfidenceNeedsReview,
		},
		{
			name: "list marker after a subsection paren — bug: multiProvisionRe was anchored to the start of tail",
			raw:  "Seksyen 4(1), 7(a), 9 dan 12 Enakmen Kesalahan Jenayah Syariah (Johor) 1997",
			want: legalcite.Parsed{ProvisionNum: iptr(4), SubProvision: iptr(1)},
			conf: legalcite.ConfidenceNeedsReview,
		},
		{
			name: "missing-space typo before dan — bug: greedy suffix letter consumed the 'd' of 'dan'",
			raw:  "Seksyen 14dan 15 Enakmen Kesalahan Jenayah Syariah (Takzir) (Terengganu) 2001",
			want: legalcite.Parsed{ProvisionNum: iptr(14)},
			conf: legalcite.ConfidenceNeedsReview,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := legalcite.Parse(tc.raw)
			if got.Confidence != tc.conf {
				t.Fatalf("Confidence = %q, want %q (parsed: %+v)", got.Confidence, tc.conf, got.Parsed)
			}
			if !parsedEqual(got.Parsed, tc.want) {
				t.Fatalf("Parsed = %+v, want %+v", got.Parsed, tc.want)
			}
		})
	}
}

func TestSortKey_OrdersSuffixCorrectly(t *testing.T) {
	// "4A" must sort after "4" but before "40" — a plain numeric/string sort
	// would put "4A" after "40".
	k4 := legalcite.SortKey(iptr(4), "")
	k4A := legalcite.SortKey(iptr(4), "A")
	k40 := legalcite.SortKey(iptr(40), "")
	if !(k4 < k4A && k4A < k40) {
		t.Fatalf("expected k4 < k4A < k40, got %q < %q < %q", k4, k4A, k40)
	}
	if legalcite.SortKey(nil, "") != "" {
		t.Fatalf("expected empty sort key for nil num")
	}
}

func intEqual(a, b *int) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	return a == nil || *a == *b
}

func parsedEqual(a, b legalcite.Parsed) bool {
	return intEqual(a.Part, b.Part) &&
		intEqual(a.Chapter, b.Chapter) &&
		intEqual(a.ProvisionNum, b.ProvisionNum) &&
		a.ProvisionSuffix == b.ProvisionSuffix &&
		intEqual(a.SubProvision, b.SubProvision) &&
		a.SubProvisionSuffix == b.SubProvisionSuffix &&
		a.Paragraph == b.Paragraph &&
		a.Subparagraph == b.Subparagraph &&
		a.SubSubparagraph == b.SubSubparagraph &&
		intEqual(a.Schedule, b.Schedule) &&
		intEqual(a.ScheduleList, b.ScheduleList)
}
