package pagination

import "testing"

func TestNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		page       int
		pageSize   int
		wantLimit  int
		wantOffset int
	}{
		{name: "defaults", page: 0, pageSize: 0, wantLimit: DefaultPageSize, wantOffset: 0},
		{name: "first page", page: 1, pageSize: 25, wantLimit: 25, wantOffset: 0},
		{name: "third page", page: 3, pageSize: 25, wantLimit: 25, wantOffset: 50},
		{name: "page size at the cap is honoured", page: 1, pageSize: MaxPageSize, wantLimit: MaxPageSize, wantOffset: 0},

		// The DoS case: an authenticated caller asking for the whole table.
		{name: "page size above the cap is clamped", page: 1, pageSize: 100_000_000, wantLimit: MaxPageSize, wantOffset: 0},

		{name: "negative page size falls back to the default", page: 1, pageSize: -5, wantLimit: DefaultPageSize, wantOffset: 0},
		{name: "negative page falls back to the first", page: -3, pageSize: 10, wantLimit: 10, wantOffset: 0},

		// (page-1)*limit used to wrap negative here, and GORM drops a negative
		// OFFSET — so this silently served page 1 for a request far past the end.
		{name: "absurd page cannot wrap the offset", page: 1 << 62, pageSize: 100, wantLimit: 100, wantOffset: maxOffset / 100 * 100},
		{name: "max int page cannot wrap the offset", page: int(^uint(0) >> 1), pageSize: 10, wantLimit: 10, wantOffset: maxOffset / 10 * 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			limit, offset := Normalize(tt.page, tt.pageSize)
			if limit != tt.wantLimit {
				t.Fatalf("Normalize(%d, %d) limit = %d, want %d", tt.page, tt.pageSize, limit, tt.wantLimit)
			}
			if offset != tt.wantOffset {
				t.Fatalf("Normalize(%d, %d) offset = %d, want %d", tt.page, tt.pageSize, offset, tt.wantOffset)
			}
			if offset < 0 {
				t.Fatalf("Normalize(%d, %d) produced a negative offset %d — GORM drops it and serves page 1", tt.page, tt.pageSize, offset)
			}
		})
	}
}

// TestNormalize_OffsetNeverNegative sweeps the page range for every page size a
// caller can reach, since a negative offset fails silently rather than loudly.
func TestNormalize_OffsetNeverNegative(t *testing.T) {
	t.Parallel()

	maxInt := int(^uint(0) >> 1)
	pages := []int{-1, 0, 1, 2, 1 << 20, 1 << 40, 1 << 62, maxInt}
	sizes := []int{-1, 0, 1, 7, DefaultPageSize, MaxPageSize, MaxPageSize + 1, maxInt}

	for _, page := range pages {
		for _, size := range sizes {
			limit, offset := Normalize(page, size)
			if limit <= 0 || limit > MaxPageSize {
				t.Fatalf("Normalize(%d, %d) limit = %d, want 1..%d", page, size, limit, MaxPageSize)
			}
			if offset < 0 {
				t.Fatalf("Normalize(%d, %d) offset = %d, want >= 0", page, size, offset)
			}
		}
	}
}
