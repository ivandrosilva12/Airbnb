package shared

// Page describes pagination parameters for list queries.
type Page struct {
	Limit  int
	Offset int
}

// NewPage normalises pagination input into safe bounds.
func NewPage(limit, offset int) Page {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return Page{Limit: limit, Offset: offset}
}

// PageResult wraps a slice of items with the total count for the query.
type PageResult[T any] struct {
	Items []T
	Total int64
}
