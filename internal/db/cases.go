package db

import (
	"context"

	"gorm.io/gorm"
)

func (s *postgresStore) CreateCase(ctx context.Context, departmentID, urlID uint, phase string, opts CaseCreateOptions) (Case, error) {
	c := Case{DepartmentID: departmentID, Status: phase, AgencyID: opts.AgencyID, DueDate: opts.DueDate}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&c).Error; err != nil {
			return err
		}
		return tx.Create(&CaseURL{CaseID: c.ID, URLID: urlID, Phase: phase}).Error
	})
	return c, err
}

// UpdateCaseURLPhase sets one (case, url) pair's own Phase — the per-domain
// override of Case.Status. False if no such CaseURL row exists (caller has
// already validated phase against urlStatusAllowed).
func (s *postgresStore) UpdateCaseURLPhase(ctx context.Context, caseID, urlID uint, phase string) (bool, error) {
	res := s.db.WithContext(ctx).
		Model(&CaseURL{}).
		Where("case_id = ? AND url_id = ?", caseID, urlID).
		Update("phase", phase)
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
	if fields.AgencyID != nil {
		updates["agency_id"] = *fields.AgencyID
	}
	if fields.Status != nil {
		updates["status"] = *fields.Status
	}
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

func (s *postgresStore) AddURLToCase(ctx context.Context, caseID, urlID uint, phase string) (CaseURL, error) {
	cu := CaseURL{CaseID: caseID, URLID: urlID, Phase: phase}
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
		Order("case_letters.letter_date desc").
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
	phaseByCaseID := make(map[uint]string, len(caseURLs))
	caseIDs := make([]uint, 0, len(caseURLs))
	for _, cu := range caseURLs {
		phaseByCaseID[cu.CaseID] = cu.Phase
		caseIDs = append(caseIDs, cu.CaseID)
	}
	if len(caseIDs) == 0 {
		return []CaseWithLetters{}, nil
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
		result = append(result, CaseWithLetters{
			Case:    c,
			Phase:   phaseByCaseID[c.ID],
			Letters: lettersByCaseID[c.ID],
		})
	}
	return result, nil
}
