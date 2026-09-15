package db

import (
	"context"

	"gorm.io/gorm"
)

func (s *postgresStore) CreateCase(ctx context.Context, departmentID, urlID uint, status string, opts CaseCreateOptions) (Case, error) {
	c := Case{DepartmentID: departmentID, DueDate: opts.DueDate}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&c).Error; err != nil {
			return err
		}
		return tx.Create(&CaseURL{CaseID: c.ID, URLID: urlID, Status: status, OriginalURL: opts.OriginalURL, AgencyID: opts.AgencyID}).Error
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
			notice.letter_date as notice_letter_date, notice.received_at as notice_received_at,
			notice.submitted_at as notice_submitted_at, notice.remarks as notice_remarks,
			memo.id as memo_letter_id, memo.subject as memo_subject,
			memo.reference_number_internal as memo_reference_number_internal`).
		Joins(`LEFT JOIN case_letters notice ON notice.id = (
			SELECT cl.id FROM case_letters cl
			WHERE cl.case_id = cases.id AND cl.type IN ('Notice', 'Notice (Uplift)')
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
	for _, r := range rows {
		i := idxByCaseID[r.CaseID]
		summaries[i].Domains = append(summaries[i].Domains, CaseSummaryDomain{
			URLID: r.URLID, URL: r.URL, Status: r.Status, OriginalURL: r.OriginalURL,
			AgencyID: r.AgencyID, AgencyName: r.AgencyName,
			Offences: offMap[r.URLID],
		})
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
