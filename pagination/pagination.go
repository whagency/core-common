package pagination

const (
	// DefaultPageSize is used when the caller asks for no particular size.
	DefaultPageSize = 10
	// MaxPageSize caps what a caller may ask for. An uncapped per-page turns a
	// cheap request into an arbitrarily expensive one: the handler is the only
	// place that knows the value came from a query string.
	MaxPageSize = 100
	// maxOffset bounds how deep a caller may page. It exists to keep the offset
	// arithmetic from overflowing, not as a product limit — see Normalize.
	maxOffset = 1<<31 - 1
)

// Normalize turns raw page / per-page query values into a LIMIT and OFFSET.
//
// It clamps rather than rejects, because these arrive as optional query
// parameters where a bad value should not fail the request.
//
// Both bounds matter. An unbounded page size lets any authenticated caller ask
// for the whole table. An unbounded page overflows (page-1)*limit into a
// negative offset, and GORM writes OFFSET only when it is positive — so the
// request silently returns the *first* page instead of an empty one. Clamping
// the page instead keeps that case honest: an absurd page returns no rows.
func Normalize(page, pageSize int) (limit, offset int) {
	limit = pageSize
	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}

	if page <= 0 {
		page = 1
	}
	// Clamp before multiplying, so the multiplication itself cannot overflow.
	if maxPage := maxOffset/limit + 1; page > maxPage {
		page = maxPage
	}

	return limit, (page - 1) * limit
}

type Pagination struct {
	ShownFrom  int
	ShownTo    int
	Page       int
	PageSize   int
	TotalCount int64
}

// GetPaginationFromParams calculates pagination details based on the provided parameters
func GetPaginationFromParams(limit, offset, listCount int, totalCount int64) Pagination {
	page := 1
	if limit > 0 {
		page = offset/limit + 1
	}
	shownFrom := 0
	shownTo := 0
	if listCount > 0 {
		shownFrom = (page-1)*limit + 1
		shownTo = (page-1)*limit + listCount
	}
	return Pagination{
		ShownFrom:  shownFrom,
		ShownTo:    shownTo,
		Page:       page,
		PageSize:   limit,
		TotalCount: totalCount,
	}
}
