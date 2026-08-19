package db

import (
	"context"

	"gorm.io/gorm"
)

func (s *postgresStore) CreateCase(ctx context.Context, departmentID, urlID uint, phase string) (Case, error) {
	c := Case{DepartmentID: departmentID}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&c).Error; err != nil {
			return err
		}
		return tx.Create(&CaseURL{CaseID: c.ID, URLID: urlID, Phase: phase}).Error
	})
	return c, err
}

func (s *postgresStore) AddCaseLetter(ctx context.Context, letter CaseLetter) (CaseLetter, error) {
	err := s.db.WithContext(ctx).Create(&letter).Error
	return letter, err
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
