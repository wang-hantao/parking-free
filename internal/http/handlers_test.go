package httpapi

import (
	"net/http"
	"testing"

	"github.com/wang-hantao/parking-free/internal/engine"
)

// TestParseAllowedQuery_ModeParam covers the `mode=` query parameter
// added for strict-segment resolution (3.1).
func TestParseAllowedQuery_ModeParam(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		want    engine.QueryMode
		wantErr bool
	}{
		{name: "absent defaults to nearby", mode: "", want: engine.QueryModeNearby},
		{name: "explicit nearby", mode: "nearby", want: engine.QueryModeNearby},
		{name: "strict", mode: "strict", want: engine.QueryModeStrict},
		{name: "unknown value rejected", mode: "exact", wantErr: true},
		{name: "case-sensitive: STRICT not accepted", mode: "STRICT", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := "http://localhost/allowed?lat=59.33&lng=18.07&plate=ABC123"
			if tc.mode != "" {
				url += "&mode=" + tc.mode
			}
			req, _ := http.NewRequest("GET", url, nil)
			q, err := parseAllowedQuery(req)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for mode=%q, got Query.Mode=%q", tc.mode, q.Mode)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if q.Mode != tc.want {
				t.Errorf("Query.Mode: want %q, got %q", tc.want, q.Mode)
			}
		})
	}
}
