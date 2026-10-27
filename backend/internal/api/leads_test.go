package api

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/model"
)

// queryContext is a gin context for a GET with the given query string.
func queryContext(rawQuery string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/api/leads?"+rawQuery, nil)
	return c
}

func TestParseLeadQueryBandAndScoreSort(t *testing.T) {
	q, errs := parseLeadQuery(queryContext("band=Hot&sort_by=score&sort_dir=asc"))
	if errs.Any() {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if q.Band != model.BandHot || q.SortBy != model.SortByScore || q.SortDir != "asc" {
		t.Errorf("got band %q, sort %q %q; want hot, score asc", q.Band, q.SortBy, q.SortDir)
	}

	// "all" and empty mean no band filter, like ?stage=all.
	for _, raw := range []string{"band=all", "band=", ""} {
		q, errs := parseLeadQuery(queryContext(raw))
		if errs.Any() || q.Band != "" {
			t.Errorf("%q: band %q, errors %v; want no filter", raw, q.Band, errs)
		}
	}

	if _, errs := parseLeadQuery(queryContext("band=lukewarm")); errs["band"] == "" {
		t.Errorf("band=lukewarm should be rejected, got %v", errs)
	}
}
