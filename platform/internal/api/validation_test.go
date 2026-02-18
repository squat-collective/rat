package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
)

// TestValidName_TableDriven exercises the validName function with a comprehensive
// set of valid and invalid inputs covering edge cases.
func TestValidName_TableDriven(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// Valid names
		{"lowercase single char", "a", true},
		{"lowercase word", "orders", true},
		{"with hyphens", "my-pipeline", true},
		{"with underscores", "my_pipeline", true},
		{"mixed hyphens and underscores", "my-pipeline_v2", true},
		{"with digits after letter", "orders123", true},
		{"letter then digit", "a1", true},
		{"max allowed length", string(make([]byte, 128)), false}, // all null bytes, not valid
		{"exactly 128 lowercase chars", makeName(128), true},

		// Invalid names
		{"empty string", "", false},
		{"starts with digit", "1orders", false},
		{"starts with hyphen", "-orders", false},
		{"starts with underscore", "_orders", false},
		{"uppercase first char", "Orders", false},
		{"mixed case", "myPipeline", false},
		{"all uppercase", "ORDERS", false},
		{"contains spaces", "my pipeline", false},
		{"contains dot", "my.pipeline", false},
		{"contains slash", "my/pipeline", false},
		{"contains at sign", "my@pipeline", false},
		{"contains special chars", "my!pipeline", false},
		{"unicode chars", "ord\u00e9rs", false},
		{"exceeds 128 chars", makeName(129), false},
		{"single digit", "1", false},
		{"single hyphen", "-", false},
		{"single underscore", "_", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validName(tt.input)
			assert.Equal(t, tt.want, got, "validName(%q)", tt.input)
		})
	}
}

// makeName generates a valid lowercase name of exactly n characters.
func makeName(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	b[0] = 'a'
	for i := 1; i < n; i++ {
		b[i] = byte('a' + (i % 26))
	}
	return string(b)
}

// TestValidLayer_TableDriven exercises domain.ValidLayer with all known layers and invalid inputs.
func TestValidLayer_TableDriven(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"bronze", "bronze", true},
		{"silver", "silver", true},
		{"gold", "gold", true},
		{"empty", "", false},
		{"platinum", "platinum", false},
		{"uppercase Bronze", "Bronze", false},
		{"uppercase GOLD", "GOLD", false},
		{"raw", "raw", false},
		{"staging", "staging", false},
		{"with space", "bronze ", false},
		{"numeric", "123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.ValidLayer(tt.input)
			assert.Equal(t, tt.want, got, "ValidLayer(%q)", tt.input)
		})
	}
}

// TestParsePagination_TableDriven exercises parsePagination with various query params.
func TestParsePagination_TableDriven(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantLimit  int
		wantOffset int
	}{
		{"no params uses defaults", "", defaultPageLimit, 0},
		{"custom limit", "limit=10", 10, 0},
		{"custom offset", "offset=5", defaultPageLimit, 5},
		{"both params", "limit=25&offset=10", 25, 10},
		{"limit exceeds max", "limit=500", maxPageLimit, 0},
		{"limit exactly at max", "limit=200", maxPageLimit, 0},
		{"limit just under max", "limit=199", 199, 0},
		{"limit zero uses default", "limit=0", defaultPageLimit, 0},
		{"negative limit uses default", "limit=-5", defaultPageLimit, 0},
		{"negative offset uses default", "offset=-1", defaultPageLimit, 0},
		{"non-numeric limit uses default", "limit=abc", defaultPageLimit, 0},
		{"non-numeric offset uses default", "offset=xyz", defaultPageLimit, 0},
		{"limit=1 is valid", "limit=1", 1, 0},
		{"very large offset", "offset=999999", defaultPageLimit, 999999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test?"+tt.query, http.NoBody)
			limit, offset := parsePagination(req)
			assert.Equal(t, tt.wantLimit, limit, "limit for query %q", tt.query)
			assert.Equal(t, tt.wantOffset, offset, "offset for query %q", tt.query)
		})
	}
}

// TestPaginate_TableDriven exercises the paginate helper function.
func TestPaginate_TableDriven(t *testing.T) {
	items := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

	tests := []struct {
		name   string
		limit  int
		offset int
		want   []int
	}{
		{"first page", 3, 0, []int{0, 1, 2}},
		{"second page", 3, 3, []int{3, 4, 5}},
		{"last partial page", 3, 9, []int{9}},
		{"offset beyond length", 3, 15, []int{}},
		{"offset at length", 3, 10, []int{}},
		{"limit larger than remaining", 20, 7, []int{7, 8, 9}},
		{"full slice", 10, 0, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := paginate(items, tt.limit, tt.offset)
			assert.Equal(t, tt.want, got)
		})
	}
}
