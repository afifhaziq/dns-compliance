package db

import (
	"context"
	"time"

	"gorm.io/gorm"
)

func (s *postgresStore) ListInstruments(ctx context.Context) ([]Instrument, error) {
	var instruments []Instrument
	return instruments, s.db.WithContext(ctx).Order("short_title asc").Find(&instruments).Error
}

// GetOrCreateInstrument finds an existing row by (type, jurisdiction,
// number, year — plus short_title when number is empty) or creates one. Year is handled via an explicit IS NULL
// branch when nil, since SQL NULL never equals NULL in a plain WHERE.
func (s *postgresStore) GetOrCreateInstrument(ctx context.Context, in Instrument) (Instrument, error) {
	q := s.db.WithContext(ctx).Where("type = ? AND jurisdiction = ? AND number = ?", in.Type, in.Jurisdiction, in.Number)
	if in.Year != nil {
		q = q.Where("year = ?", *in.Year)
	} else {
		q = q.Where("year IS NULL")
	}
	// ponytail: unnumbered Acts sharing a year (Poisons vs Dangerous Drugs,
	// 1952) would otherwise collapse onto whichever was created first, so
	// fall back to title when there's no number to tell them apart.
	if in.Number == "" {
		q = q.Where("short_title = ?", in.ShortTitle)
	}
	var existing Instrument
	err := q.Attrs(in).FirstOrCreate(&existing).Error
	return existing, err
}

func (s *postgresStore) UpdateInstrument(ctx context.Context, id uint, in Instrument) (Instrument, error) {
	in.ID = id
	err := s.db.WithContext(ctx).Model(&Instrument{}).Where("id = ?", id).
		Updates(map[string]any{
			"type": in.Type, "jurisdiction": in.Jurisdiction, "number": in.Number,
			"year": in.Year, "short_title": in.ShortTitle,
		}).Error
	return in, err
}

func (s *postgresStore) DeleteInstrument(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&Instrument{}, id).Error
}

// ListCitationsByInstrument preloads Instrument — the frontend's citation
// edit flow needs the parent instrument_id for the update payload, and this
// list is small (one instrument's citations) so preloading it unconditionally
// is cheap.
func (s *postgresStore) ListCitationsByInstrument(ctx context.Context, instrumentID uint) ([]Citation, error) {
	var citations []Citation
	return citations, s.db.WithContext(ctx).Preload("Instrument").Where("instrument_id = ?", instrumentID).Order("sort_key asc").Find(&citations).Error
}

func (s *postgresStore) CreateCitation(ctx context.Context, c Citation) (Citation, error) {
	c.ID = 0
	c.SortKey = BuildProvisionSortKey(c.Parsed.ProvisionNum, c.Parsed.ProvisionSuffix)
	return c, s.db.WithContext(ctx).Create(&c).Error
}

// UpdateCitation goes through GORM's struct-based Save (not a map Updates)
// specifically so the Parsed field's jsonb serializer runs — a map update
// bypasses per-field serializers and would try to hand the driver a raw Go
// struct for the parsed column.
func (s *postgresStore) UpdateCitation(ctx context.Context, id uint, c Citation) (Citation, error) {
	c.ID = id
	c.SortKey = BuildProvisionSortKey(c.Parsed.ProvisionNum, c.Parsed.ProvisionSuffix)
	err := s.db.WithContext(ctx).
		Select("InstrumentID", "RawText", "Parsed", "SortKey", "ParseConfidence").
		Save(&c).Error
	return c, err
}

func (s *postgresStore) DeleteCitation(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&Citation{}, id).Error
}

// ListCategoriesByCitation preloads Citation for the same reason
// ListCitationsByInstrument preloads Instrument — the frontend's category
// edit flow needs it for display, not just the raw citation_id.
func (s *postgresStore) ListCategoriesByCitation(ctx context.Context, citationID uint) ([]Category, error) {
	var categories []Category
	return categories, s.db.WithContext(ctx).Preload("Citation").Where("citation_id = ?", citationID).Order("name asc").Find(&categories).Error
}

// ListAllCitations/ListAllCategories/ListAllElements/ListAllSubElements back
// the legal-citations catalog page's tree, which loads the whole
// Instrument->Citation->Category->Element->SubElement hierarchy up front
// (see legal-citations.tsx's loadTree doc comment). Walking it via the
// parent-scoped List*By* methods above means one request per node at every
// level (~500+ round trips on the real catalog) before the tree can render
// at all; these four flat queries replace that with one request per level,
// and the frontend groups the results by parent id client-side.
func (s *postgresStore) ListAllCitations(ctx context.Context) ([]Citation, error) {
	var citations []Citation
	return citations, s.db.WithContext(ctx).Preload("Instrument").Order("sort_key asc").Find(&citations).Error
}

func (s *postgresStore) ListAllCategories(ctx context.Context) ([]Category, error) {
	var categories []Category
	return categories, s.db.WithContext(ctx).Preload("Citation").Order("name asc").Find(&categories).Error
}

func (s *postgresStore) CreateCategory(ctx context.Context, cat Category) (Category, error) {
	cat.ID = 0
	return cat, s.db.WithContext(ctx).Create(&cat).Error
}

func (s *postgresStore) UpdateCategory(ctx context.Context, id uint, name string) (Category, error) {
	err := s.db.WithContext(ctx).Model(&Category{}).Where("id = ?", id).Update("name", name).Error
	if err != nil {
		return Category{}, err
	}
	var cat Category
	return cat, s.db.WithContext(ctx).First(&cat, id).Error
}

func (s *postgresStore) DeleteCategory(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&Category{}, id).Error
}

func (s *postgresStore) ListElementsByCategory(ctx context.Context, categoryID uint) ([]Element, error) {
	var elements []Element
	return elements, s.db.WithContext(ctx).Where("category_id = ?", categoryID).Order("name asc").Find(&elements).Error
}

func (s *postgresStore) ListAllElements(ctx context.Context) ([]Element, error) {
	var elements []Element
	return elements, s.db.WithContext(ctx).Order("name asc").Find(&elements).Error
}

func (s *postgresStore) CreateElement(ctx context.Context, el Element) (Element, error) {
	el.ID = 0
	return el, s.db.WithContext(ctx).Create(&el).Error
}

func (s *postgresStore) UpdateElement(ctx context.Context, id uint, name string) (Element, error) {
	err := s.db.WithContext(ctx).Model(&Element{}).Where("id = ?", id).Update("name", name).Error
	if err != nil {
		return Element{}, err
	}
	var el Element
	return el, s.db.WithContext(ctx).First(&el, id).Error
}

func (s *postgresStore) DeleteElement(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&Element{}, id).Error
}

func (s *postgresStore) ListSubElementsByElement(ctx context.Context, elementID uint) ([]SubElement, error) {
	var subElements []SubElement
	return subElements, s.db.WithContext(ctx).Where("element_id = ?", elementID).Order("name asc").Find(&subElements).Error
}

func (s *postgresStore) ListAllSubElements(ctx context.Context) ([]SubElement, error) {
	var subElements []SubElement
	return subElements, s.db.WithContext(ctx).Order("name asc").Find(&subElements).Error
}

func (s *postgresStore) CreateSubElement(ctx context.Context, se SubElement) (SubElement, error) {
	se.ID = 0
	return se, s.db.WithContext(ctx).Create(&se).Error
}

func (s *postgresStore) UpdateSubElement(ctx context.Context, id uint, name string) (SubElement, error) {
	err := s.db.WithContext(ctx).Model(&SubElement{}).Where("id = ?", id).Update("name", name).Error
	if err != nil {
		return SubElement{}, err
	}
	var se SubElement
	return se, s.db.WithContext(ctx).First(&se, id).Error
}

func (s *postgresStore) DeleteSubElement(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&SubElement{}, id).Error
}

func (s *postgresStore) ListOffencesByURL(ctx context.Context, urlValue string) ([]URLOffence, error) {
	var offences []URLOffence
	err := s.db.WithContext(ctx).
		Joins("JOIN urls ON urls.id = url_offences.url_id").
		Where("urls.url = ?", urlValue).
		Preload("Category.Citation.Instrument").
		Preload("Element").
		Preload("SubElement").
		Order("url_offences.recorded_at desc").
		Find(&offences).Error
	return offences, err
}

// GetOffence preloads URL — used by the detach handler to resolve the
// owning department before deleting. Returns nil, nil if not found.
func (s *postgresStore) GetOffence(ctx context.Context, id uint) (*URLOffence, error) {
	var o URLOffence
	err := s.db.WithContext(ctx).Preload("URL").First(&o, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (s *postgresStore) AttachOffenceToURL(ctx context.Context, urlValue string, categoryID uint, elementID *uint, subElementID *uint) (URLOffence, error) {
	u, err := s.GetURLByValue(ctx, urlValue)
	if err != nil {
		return URLOffence{}, err
	}
	if u == nil {
		return URLOffence{}, gorm.ErrRecordNotFound
	}
	o := URLOffence{URLID: u.ID, CategoryID: categoryID, ElementID: elementID, SubElementID: subElementID, RecordedAt: time.Now()}
	return o, s.db.WithContext(ctx).Create(&o).Error
}

func (s *postgresStore) DetachOffenceFromURL(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&URLOffence{}, id).Error
}
