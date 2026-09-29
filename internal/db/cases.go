package db

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ErrLastCaseURL is returned by RemoveURLFromCase when the url is the
// case's only one.
var ErrLastCaseURL = errors.New("cannot remove a case's last domain")

func (s *postgresStore) CreateCase(ctx context.Context, departmentID, urlID uint, status string, opts CaseCreateOptions) (Case, error) {
	c := Case{DepartmentID: departmentID, DueDate: opts.DueDate}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&c).Error; err != nil {
			return err
		}
		if err := tx.Create(&CaseURL{CaseID: c.ID, URLID: urlID, Status: status, OriginalURL: opts.OriginalURL, AgencyID: opts.AgencyID}).Error; err != nil {
			return err
		}
		return ensureDepartmentURL(tx, departmentID, urlID)
	})
	return c, err
}

// UpdateCaseURLStatus sets one (case, url) pair's own Status. False if no
// such CaseURL row exists (caller has already validated status against
// urlStatusAllowed).
func (s *postgresStore) UpdateCaseURLStatus(ctx context.Context, caseID, urlID uint, status string) (bool, error) {
	res := s.db.WithContext(ctx).
		Model(&CaseURL{}).
		Where("case_id = ? AND url_id = ?", caseID, urlID).
		Update("status", status)
	return res.RowsAffected > 0, res.Error
}

// UpdateCaseURLAgency sets one (case, url) pair's own AgencyID. False if no
// such CaseURL row exists.
func (s *postgresStore) UpdateCaseURLAgency(ctx context.Context, caseID, urlID uint, agencyID *uint) (bool, error) {
	res := s.db.WithContext(ctx).
		Model(&CaseURL{}).
		Where("case_id = ? AND url_id = ?", caseID, urlID).
		Update("agency_id", agencyID)
	return res.RowsAffected > 0, res.Error
}

// RemoveURLFromCase — see Store.RemoveURLFromCase.
func (s *postgresStore) RemoveURLFromCase(ctx context.Context, caseID, urlID uint) (bool, error) {
	found := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&CaseURL{}).Where("case_id = ?", caseID).Count(&n).Error; err != nil {
			return err
		}
		res := tx.Where("case_id = ? AND url_id = ?", caseID, urlID).Delete(&CaseURL{})
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		if n <= 1 {
			return ErrLastCaseURL // rolls the delete back
		}
		found = true
		return tx.Where("case_id = ? AND url_id = ?", caseID, urlID).Delete(&URLOffence{}).Error
	})
	return found, err
}

// UpdateCaseFields applies a partial update to a case's shared fields —
// only non-nil fields in `fields` are touched. Ownership (departmentID owns
// caseID) is already checked by the caller (handler layer); departmentID is
// accepted here to mirror the old UpdateURLCaseFields signature but isn't
// used to scope the write.
func (s *postgresStore) UpdateCaseFields(ctx context.Context, departmentID, caseID uint, fields CaseFields) (bool, error) {
	_ = departmentID
	var count int64
	if err := s.db.WithContext(ctx).Model(&Case{}).Where("id = ?", caseID).Count(&count).Error; err != nil {
		return false, err
	}
	if count == 0 {
		return false, nil
	}

	updates := map[string]interface{}{}
	if fields.DueDate != nil {
		updates["due_date"] = *fields.DueDate
	}
	if fields.RequestedAt != nil {
		updates["requested_at"] = *fields.RequestedAt
	}
	if len(updates) == 0 {
		return true, nil // exists, but nothing in the body to apply
	}
	res := s.db.WithContext(ctx).Model(&Case{}).Where("id = ?", caseID).Updates(updates)
	return res.RowsAffected > 0, res.Error
}

func (s *postgresStore) AddCaseLetter(ctx context.Context, letter CaseLetter) (CaseLetter, error) {
	err := s.db.WithContext(ctx).Create(&letter).Error
	return letter, err
}

// ListCaseURLIDs returns every URL id a case covers, via case_urls — used
// to fan a case-level DueDate change out to a per-(department, url)
// due-date-reached task for each url the case links (see UpdateCase).
func (s *postgresStore) ListCaseURLIDs(ctx context.Context, caseID uint) ([]uint, error) {
	var ids []uint
	err := s.db.WithContext(ctx).Model(&CaseURL{}).Where("case_id = ?", caseID).Pluck("url_id", &ids).Error
	return ids, err
}

func (s *postgresStore) GetCase(ctx context.Context, id uint) (Case, error) {
	var c Case
	err := s.db.WithContext(ctx).First(&c, id).Error
	return c, err
}

func (s *postgresStore) AddURLToCase(ctx context.Context, caseID, urlID uint, status, originalURL string, agencyID *uint) (CaseURL, error) {
	cu := CaseURL{CaseID: caseID, URLID: urlID, Status: status, OriginalURL: originalURL, AgencyID: agencyID}
	err := s.db.WithContext(ctx).Create(&cu).Error
	return cu, err
}

func (s *postgresStore) ListCaseLetters(ctx context.Context, page, pageSize int) ([]CaseLetterEntry, int, error) {
	return s.listCaseLetters(ctx, page, pageSize, nil)
}

func (s *postgresStore) ListCaseLettersForDepartment(ctx context.Context, page, pageSize int, departmentID uint) ([]CaseLetterEntry, int, error) {
	return s.listCaseLetters(ctx, page, pageSize, &departmentID)
}

// caseLetterQuery returns a fresh case_letters query (optionally
// department-scoped) each call, so the count query and the paginated
// select below never share mutated clause state — same reasoning as
// domainSummaryQuery in postgres.go.
func (s *postgresStore) caseLetterQuery(ctx context.Context, departmentID *uint) *gorm.DB {
	q := s.db.WithContext(ctx).
		Table("case_letters").
		Joins("JOIN cases ON cases.id = case_letters.case_id")
	if departmentID != nil {
		q = q.Where("cases.department_id = ?", *departmentID)
	}
	return q
}

func (s *postgresStore) listCaseLetters(ctx context.Context, page, pageSize int, departmentID *uint) ([]CaseLetterEntry, int, error) {
	if page < 1 {
		page = 1
	}

	var total int64
	if err := s.caseLetterQuery(ctx, departmentID).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var entries []CaseLetterEntry
	err := s.caseLetterQuery(ctx, departmentID).
		Joins("JOIN departments ON departments.id = cases.department_id").
		Select("case_letters.*, cases.department_id AS department_id, departments.name AS department_name").
		Order("case_letters.letter_date desc, case_letters.id desc").
		Limit(pageSize).Offset((page - 1) * pageSize).
		Scan(&entries).Error
	if err != nil {
		return nil, 0, err
	}
	if len(entries) == 0 {
		return entries, int(total), nil
	}

	caseIDs := make([]uint, len(entries))
	for i, e := range entries {
		caseIDs[i] = e.CaseID
	}
	type urlRow struct {
		CaseID uint
		URL    string
	}
	var urlRows []urlRow
	if err := s.db.WithContext(ctx).
		Table("case_urls").
		Select("case_urls.case_id AS case_id, urls.url AS url").
		Joins("JOIN urls ON urls.id = case_urls.url_id").
		Where("case_urls.case_id IN ?", caseIDs).
		Scan(&urlRows).Error; err != nil {
		return nil, 0, err
	}
	urlsByCaseID := make(map[uint][]string, len(caseIDs))
	for _, r := range urlRows {
		urlsByCaseID[r.CaseID] = append(urlsByCaseID[r.CaseID], r.URL)
	}
	for i := range entries {
		entries[i].URLs = urlsByCaseID[entries[i].CaseID]
	}
	return entries, int(total), nil
}

func (s *postgresStore) ListCasesForURL(ctx context.Context, urlValue string) ([]CaseWithLetters, error) {
	u, err := s.GetURLByValue(ctx, urlValue)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, gorm.ErrRecordNotFound
	}

	var caseURLs []CaseURL
	if err := s.db.WithContext(ctx).Where("url_id = ?", u.ID).Find(&caseURLs).Error; err != nil {
		return nil, err
	}
	statusByCaseID := make(map[uint]string, len(caseURLs))
	agencyIDByCaseID := make(map[uint]*uint, len(caseURLs))
	caseIDs := make([]uint, 0, len(caseURLs))
	agencyIDs := make([]uint, 0, len(caseURLs))
	for _, cu := range caseURLs {
		statusByCaseID[cu.CaseID] = cu.Status
		agencyIDByCaseID[cu.CaseID] = cu.AgencyID
		caseIDs = append(caseIDs, cu.CaseID)
		if cu.AgencyID != nil {
			agencyIDs = append(agencyIDs, *cu.AgencyID)
		}
	}
	if len(caseIDs) == 0 {
		return []CaseWithLetters{}, nil
	}

	agencyNameByID := make(map[uint]string, len(agencyIDs))
	if len(agencyIDs) > 0 {
		var agencies []Agency
		if err := s.db.WithContext(ctx).Where("id IN ?", agencyIDs).Find(&agencies).Error; err != nil {
			return nil, err
		}
		for _, a := range agencies {
			agencyNameByID[a.ID] = a.Name
		}
	}

	var cases []Case
	if err := s.db.WithContext(ctx).Where("id IN ?", caseIDs).Find(&cases).Error; err != nil {
		return nil, err
	}
	var letters []CaseLetter
	if err := s.db.WithContext(ctx).Where("case_id IN ?", caseIDs).
		Order("letter_date desc").Find(&letters).Error; err != nil {
		return nil, err
	}
	lettersByCaseID := make(map[uint][]CaseLetter, len(cases))
	for _, l := range letters {
		lettersByCaseID[l.CaseID] = append(lettersByCaseID[l.CaseID], l)
	}

	result := make([]CaseWithLetters, 0, len(cases))
	for _, c := range cases {
		agencyID := agencyIDByCaseID[c.ID]
		var agencyName string
		if agencyID != nil {
			agencyName = agencyNameByID[*agencyID]
		}
		result = append(result, CaseWithLetters{
			Case:       c,
			Status:     statusByCaseID[c.ID],
			AgencyID:   agencyID,
			AgencyName: agencyName,
			Letters:    lettersByCaseID[c.ID],
		})
	}
	return result, nil
}

// caseSummaryQuery is the shared Select/Joins for the Cases view's per-case
// row, optionally scoped to one department — used by both ListCases and
// ListCasesForDepartment (mirrors caseLetterQuery's department-scoping
// pattern above, and postgresStore.ListDepartmentURLs's latest-Notice/
// latest-Memo correlated-subquery pattern in postgres.go).
func (s *postgresStore) caseSummaryQuery(ctx context.Context, departmentID *uint) *gorm.DB {
	q := s.db.WithContext(ctx).
		Table("cases").
		Select(`cases.id,
			cases.due_date, cases.requested_at, cases.created_at,
			notice.id as notice_letter_id, notice.subject as notice_subject,
			notice.workflow_status as notice_workflow_status,
			notice.reference_number_external as notice_reference_number_external,
			notice.reference_number_internal as notice_reference_number_internal,
			notice.recipient as notice_recipient, notice.requestor as notice_requestor,
			notice.letter_date as notice_letter_date, uplift.letter_date as uplift_letter_date, notice.received_at as notice_received_at,
			notice.submitted_at as notice_submitted_at, notice.remarks as notice_remarks,
			memo.id as memo_letter_id, memo.subject as memo_subject,
			memo.reference_number_internal as memo_reference_number_internal`).
		Joins(`LEFT JOIN case_letters notice ON notice.id = (
			SELECT cl.id FROM case_letters cl
			WHERE cl.case_id = cases.id AND cl.type IN ('Notice', 'Notice (Uplift)')
			ORDER BY (cl.type = 'Notice') DESC, cl.letter_date DESC LIMIT 1)`).
		Joins(`LEFT JOIN case_letters uplift ON uplift.id = (
			SELECT cl.id FROM case_letters cl
			WHERE cl.case_id = cases.id AND cl.type = 'Notice (Uplift)'
			ORDER BY cl.letter_date DESC LIMIT 1)`).
		Joins(`LEFT JOIN case_letters memo ON memo.id = (
			SELECT cl.id FROM case_letters cl
			WHERE cl.case_id = cases.id AND cl.type IN ('Memo', 'Memo (Uplift)')
			ORDER BY cl.letter_date DESC LIMIT 1)`)
	if departmentID != nil {
		q = q.Where("cases.department_id = ?", *departmentID)
	}
	return q
}

func (s *postgresStore) listCaseSummaries(ctx context.Context, departmentID *uint) ([]CaseSummary, error) {
	var summaries []CaseSummary
	if err := s.caseSummaryQuery(ctx, departmentID).Order("cases.created_at desc").Scan(&summaries).Error; err != nil {
		return nil, err
	}
	return s.attachCaseDomains(ctx, summaries)
}

// DateFilter is one Cases-view date filter chip: Op is on/before/after/
// between, From/To are YYYY-MM-DD (To only for between). Compared on the UTC
// calendar day, same as the old client-side filter compared ISO prefixes.
type DateFilter struct{ Op, From, To string }

// CaseListParams drives ListCaseSummariesPage. Zero values mean "no filter".
type CaseListParams struct {
	DepartmentID   *uint // RBAC scope; nil = global (admin)
	Page, PageSize int
	Query          string // case-insensitive: case id, notice refs or any domain
	Status         string // some domain has this status
	AgencyID       *uint  // some domain has this agency
	RequestingDept *uint  // some domain is also covered by a case of this department
	Created, Due   DateFilter
	SortBy         string // "id" | "due_date" | "notice_letter_date" | "uplift_letter_date"; default newest-created first
	SortDesc       bool
}

// dayRange turns a DateFilter into a half-open [from, to) UTC range so the
// comparison is a plain timestamp range check on both Postgres and SQLite.
// ok=false when the chip has no usable date yet (filter is skipped).
func (f DateFilter) dayRange() (from, to *time.Time, ok bool) {
	parse := func(s string) *time.Time {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return nil
		}
		return &t
	}
	a, b := parse(f.From), parse(f.To)
	next := func(t *time.Time) *time.Time { n := t.AddDate(0, 0, 1); return &n }
	switch {
	case a == nil && b == nil:
		return nil, nil, false
	case f.Op == "on" && a != nil:
		return a, next(a), true
	case f.Op == "before" && a != nil:
		return nil, a, true
	case f.Op == "after" && a != nil:
		return next(a), nil, true
	case f.Op == "between":
		if b != nil {
			b = next(b)
		}
		return a, b, true
	}
	return nil, nil, false
}

func applyDateFilter(q *gorm.DB, col string, f DateFilter) *gorm.DB {
	from, to, ok := f.dayRange()
	if !ok {
		return q
	}
	if from != nil {
		q = q.Where(col+" >= ?", *from)
	}
	if to != nil {
		q = q.Where(col+" < ?", *to)
	}
	return q
}

// ListCaseSummariesPage is the Cases view's server-side paged/filtered/sorted
// list. Returns the page's cases (domains attached) and the total number of
// cases matching the filters.
func (s *postgresStore) ListCaseSummariesPage(ctx context.Context, p CaseListParams) ([]CaseSummary, int, error) {
	q := s.caseSummaryQuery(ctx, p.DepartmentID)
	if qs := strings.ToLower(strings.TrimSpace(p.Query)); qs != "" {
		like := "%" + qs + "%"
		// A bare number (optionally "#"-prefixed, as the table shows it) also matches the case id.
		caseID, _ := strconv.ParseUint(strings.TrimPrefix(qs, "#"), 10, 64)
		q = q.Where(`cases.id = ? OR LOWER(notice.reference_number_external) LIKE ? OR LOWER(notice.reference_number_internal) LIKE ?
			OR EXISTS (SELECT 1 FROM case_urls cu JOIN urls u ON u.id = cu.url_id WHERE cu.case_id = cases.id AND LOWER(u.url) LIKE ?)`, caseID, like, like, like)
	}
	if p.Status != "" {
		q = q.Where("EXISTS (SELECT 1 FROM case_urls cu WHERE cu.case_id = cases.id AND cu.status = ?)", p.Status)
	}
	if p.AgencyID != nil {
		q = q.Where("EXISTS (SELECT 1 FROM case_urls cu WHERE cu.case_id = cases.id AND cu.agency_id = ?)", *p.AgencyID)
	}
	if p.RequestingDept != nil {
		q = q.Where(`EXISTS (SELECT 1 FROM case_urls cu
			JOIN case_urls cu2 ON cu2.url_id = cu.url_id JOIN cases c2 ON c2.id = cu2.case_id
			WHERE cu.case_id = cases.id AND c2.department_id = ?)`, *p.RequestingDept)
	}
	q = applyDateFilter(q, "cases.created_at", p.Created)
	q = applyDateFilter(q, "cases.due_date", p.Due)

	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	dir := "desc"
	if !p.SortDesc && p.SortBy != "" {
		dir = "asc"
	}
	switch p.SortBy {
	case "id":
		q = q.Order("cases.id " + dir)
	case "due_date", "notice_letter_date", "uplift_letter_date":
		col := map[string]string{"due_date": "cases.due_date", "notice_letter_date": "notice.letter_date", "uplift_letter_date": "uplift.letter_date"}[p.SortBy]
		q = q.Order(col + " IS NULL").Order(col + " " + dir).Order("cases.id desc") // empty dates always last
	default:
		q = q.Order("cases.created_at desc, cases.id desc")
	}
	var summaries []CaseSummary
	if err := q.Limit(p.PageSize).Offset((p.Page - 1) * p.PageSize).Scan(&summaries).Error; err != nil {
		return nil, 0, err
	}
	out, err := s.attachCaseDomains(ctx, summaries)
	return out, int(total), err
}

// attachCaseDomains loads each summary's domains (with per-domain agency and
// offences) in three queries total, however many summaries there are.
func (s *postgresStore) attachCaseDomains(ctx context.Context, summaries []CaseSummary) ([]CaseSummary, error) {
	if len(summaries) == 0 {
		return summaries, nil
	}

	caseIDs := make([]uint, len(summaries))
	idxByCaseID := make(map[uint]int, len(summaries))
	for i, c := range summaries {
		caseIDs[i] = c.ID
		idxByCaseID[c.ID] = i
	}
	type domainRow struct {
		CaseID      uint
		URLID       uint
		URL         string
		Status      string
		OriginalURL string
		AgencyID    *uint
		AgencyName  string
	}
	var rows []domainRow
	if err := s.db.WithContext(ctx).
		Table("case_urls").
		Select(`case_urls.case_id as case_id, case_urls.url_id as url_id, urls.url as url,
			case_urls.status as status, case_urls.original_url as original_url,
			case_urls.agency_id as agency_id, agencies.name as agency_name`).
		Joins("JOIN urls ON urls.id = case_urls.url_id").
		Joins("LEFT JOIN agencies ON agencies.id = case_urls.agency_id").
		Where("case_urls.case_id IN ?", caseIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	domainURLIDs := make([]uint, len(rows))
	for i, r := range rows {
		domainURLIDs[i] = r.URLID
	}
	offMap, err := s.offencesByURLIDs(ctx, domainURLIDs)
	if err != nil {
		return nil, err
	}
	caseOffMap, err := s.offencesByCase(ctx, caseIDs)
	if err != nil {
		return nil, err
	}
	// Latest scan run's per-domain outcome (same run the Results page shows).
	type scanAgg struct{ Total, Compliant int }
	scanByURL := map[uint]scanAgg{}
	var scannedAt *time.Time
	if run, err := s.LastScanRun(ctx); err != nil {
		return nil, err
	} else if run != nil {
		var aggs []struct {
			URLID     uint
			Total     int
			Compliant int
		}
		if err := s.db.WithContext(ctx).Table("scan_results").
			Select("url_id, COUNT(*) AS total, SUM(CASE WHEN compliant THEN 1 ELSE 0 END) AS compliant").
			Where("scan_run_id = ? AND url_id IN ?", run.ID, domainURLIDs).
			Group("url_id").Scan(&aggs).Error; err != nil {
			return nil, err
		}
		for _, a := range aggs {
			scanByURL[a.URLID] = scanAgg{a.Total, a.Compliant}
		}
		scannedAt = &run.StartedAt
	}
	for _, r := range rows {
		i := idxByCaseID[r.CaseID]
		// This case's own offences; legacy rows with no case link fall back
		// to the url-level (distinct) set.
		offences := caseOffMap[[2]uint{r.CaseID, r.URLID}]
		if len(offences) == 0 {
			offences = offMap[r.URLID]
		}
		d := CaseSummaryDomain{
			URLID: r.URLID, URL: r.URL, Status: r.Status, OriginalURL: r.OriginalURL,
			AgencyID: r.AgencyID, AgencyName: r.AgencyName,
			Offences: offences, ScanTotal: scanByURL[r.URLID].Total, ScanCompliant: scanByURL[r.URLID].Compliant,
		}
		if d.ScanTotal > 0 {
			d.ScannedAt = scannedAt
		}
		summaries[i].Domains = append(summaries[i].Domains, d)
	}
	return summaries, nil
}

func (s *postgresStore) ListCases(ctx context.Context) ([]CaseSummary, error) {
	return s.listCaseSummaries(ctx, nil)
}

func (s *postgresStore) ListCasesForDepartment(ctx context.Context, departmentID uint) ([]CaseSummary, error) {
	return s.listCaseSummaries(ctx, &departmentID)
}

// UpdateCaseLetterFields applies a partial update to one CaseLetter's
// fields, scoped by (caseID, letterID) — see CaseStore's doc comment.
func (s *postgresStore) UpdateCaseLetterFields(ctx context.Context, caseID, letterID uint, fields CaseLetterFields) (bool, error) {
	updates := map[string]interface{}{}
	if fields.Subject != nil {
		updates["subject"] = *fields.Subject
	}
	if fields.WorkflowStatus != nil {
		updates["workflow_status"] = *fields.WorkflowStatus
	}
	if fields.ReferenceNumberExternal != nil {
		updates["reference_number_external"] = *fields.ReferenceNumberExternal
	}
	if fields.ReferenceNumberInternal != nil {
		updates["reference_number_internal"] = *fields.ReferenceNumberInternal
	}
	if fields.Recipient != nil {
		updates["recipient"] = *fields.Recipient
	}
	if fields.Requestor != nil {
		updates["requestor"] = *fields.Requestor
	}
	if fields.Remarks != nil {
		updates["remarks"] = *fields.Remarks
	}
	if fields.LetterDate != nil {
		updates["letter_date"] = *fields.LetterDate
	}
	if fields.ReceivedAt != nil {
		updates["received_at"] = *fields.ReceivedAt
	}
	if fields.SubmittedAt != nil {
		updates["submitted_at"] = *fields.SubmittedAt
	}
	if fields.OICUserID != nil {
		updates["oic_user_id"] = *fields.OICUserID
	}
	if len(updates) == 0 {
		var count int64
		if err := s.db.WithContext(ctx).Model(&CaseLetter{}).Where("id = ? AND case_id = ?", letterID, caseID).Count(&count).Error; err != nil {
			return false, err
		}
		return count > 0, nil
	}
	res := s.db.WithContext(ctx).Model(&CaseLetter{}).Where("id = ? AND case_id = ?", letterID, caseID).Updates(updates)
	return res.RowsAffected > 0, res.Error
}

// DeleteCaseLetter removes one CaseLetter, scoped by (caseID, letterID) —
// see CaseStore's doc comment.
func (s *postgresStore) DeleteCaseLetter(ctx context.Context, caseID, letterID uint) (bool, error) {
	res := s.db.WithContext(ctx).Where("id = ? AND case_id = ?", letterID, caseID).Delete(&CaseLetter{})
	return res.RowsAffected > 0, res.Error
}

// BlockingStatRow is one (year, agency, offence) bucket of blocked domains.
// Year is 0 when the case has no dated Notice letter.
type BlockingStatRow struct {
	Year    int    `json:"year"`
	Agency  string `json:"agency"`
	Offence string `json:"offence"`
	Count   int    `json:"count"`
}

// BlockingStats counts (case, domain) blocks currently in status blocked —
// matching the MCMC stats workbook, which leaves uplifted/suspended blocks
// out — by Notice-letter year, agency and offence category. A domain
// carrying several offences on one case counts once under each distinct
// category (not once per offence row: a compound "Seksyen 211 dan 233"
// citation or a split element gives the same category several rows).
// `categories` has multiple rows for what's really the same offence but
// differently cased (e.g. "Tidak Berdaftar" vs "Tidak berdaftar") — the SQL
// groups by the raw name (so it stays untouched by the offence-casing fix),
// and MergeOffenceCasing folds those variants together afterward.
// ponytail: Postgres-only (EXTRACT); no SQLite test.
func (s *postgresStore) BlockingStats(ctx context.Context, departmentID *uint) ([]BlockingStatRow, error) {
	q := `SELECT COALESCE(EXTRACT(YEAR FROM (SELECT MIN(cl.letter_date) FROM case_letters cl
		WHERE cl.case_id = cu.case_id AND cl.type = 'Notice'))::int, 0) AS year,
	  COALESCE(a.name, 'Unassigned') AS agency,
	  COALESCE(cat.name, 'Unclassified') AS offence,
	  COUNT(DISTINCT (cu.case_id, cu.url_id)) AS count
	FROM case_urls cu
	JOIN cases c ON c.id = cu.case_id
	LEFT JOIN agencies a ON a.id = cu.agency_id
	LEFT JOIN url_offences o ON o.case_id = cu.case_id AND o.url_id = cu.url_id
	LEFT JOIN categories cat ON cat.id = o.category_id
	WHERE cu.status = 'blocked'`
	args := []any{}
	if departmentID != nil {
		q += " AND c.department_id = ?"
		args = append(args, *departmentID)
	}
	q += " GROUP BY 1, 2, 3"
	var rows []BlockingStatRow
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return MergeOffenceCasing(rows), nil
}

// MergeOffenceCasing folds BlockingStatRow buckets that differ only in the
// offence name's casing into one. The display name is picked globally per
// offence — the casing variant with the highest total count across every
// row, ties broken alphabetically (which also happens to prefer Title Case,
// since uppercase sorts before lowercase in ASCII) — deterministic either
// way, and crucially *not* decided separately per (year, agency) bucket:
// picking it per-bucket let the winning casing flip from one year to the
// next for the same agency, which just re-split the offence in the
// frontend's own (agency, offence)-string grouping instead of fixing it.
// Exported for unit testing without a database (parallels
// db.DailyComplianceLevel).
func MergeOffenceCasing(rows []BlockingStatRow) []BlockingStatRow {
	// Pass 1: total each exact casing variant's count within its
	// case-insensitive group, to find the group's overall winner.
	variantCounts := make(map[string]map[string]int)
	for _, r := range rows {
		lower := strings.ToLower(r.Offence)
		if variantCounts[lower] == nil {
			variantCounts[lower] = make(map[string]int)
		}
		variantCounts[lower][r.Offence] += r.Count
	}
	displayName := make(map[string]string, len(variantCounts))
	for lower, variants := range variantCounts {
		best, bestCount := "", -1
		for name, count := range variants {
			if count > bestCount || (count == bestCount && name < best) {
				best, bestCount = name, count
			}
		}
		displayName[lower] = best
	}

	// Pass 2: merge same-bucket duplicates (a year/agency can itself carry
	// both casings, e.g. two cases in the same year classified differently)
	// and apply the group's single display name throughout.
	type key struct {
		year   int
		agency string
		lower  string
	}
	order := make([]key, 0, len(rows))
	merged := make(map[key]*BlockingStatRow, len(rows))
	for _, r := range rows {
		lower := strings.ToLower(r.Offence)
		k := key{r.Year, r.Agency, lower}
		if existing, ok := merged[k]; ok {
			existing.Count += r.Count
			continue
		}
		merged[k] = &BlockingStatRow{Year: r.Year, Agency: r.Agency, Offence: displayName[lower], Count: r.Count}
		order = append(order, k)
	}
	out := make([]BlockingStatRow, 0, len(order))
	for _, k := range order {
		out = append(out, *merged[k])
	}
	return out
}

// AppendOriginalURL adds one more cited text to a CaseURL.OriginalURL. A case
// links a normalized host only once, so when it cites several paths on the
// same host ("linktr.ee/a", "linktr.ee/b") they share one row and every
// distinct text is kept, ", "-joined, for the paper trail.
func AppendOriginalURL(existing, add string) string {
	add = strings.TrimSpace(add)
	if add == "" {
		return existing
	}
	if existing == "" {
		return add
	}
	for _, s := range strings.Split(existing, ", ") {
		if s == add {
			return existing
		}
	}
	return existing + ", " + add
}
