package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS_NoAllowedOrigins_NoOp(t *testing.T) {
	mw := cors(nil)
	called := false
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))

	req := httptest.NewRequest("GET", "/allowed", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if !called {
		t.Errorf("inner handler should run")
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("no-op CORS should not set Access-Control-Allow-Origin; got %q", got)
	}
}

func TestCORS_MatchingOrigin_HeadersSet(t *testing.T) {
	mw := cors([]string{"https://parkingfree.io", "http://localhost:5173"})
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

	req := httptest.NewRequest("GET", "/allowed", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("want allow-origin = http://localhost:5173, got %q", got)
	}
	if got := rr.Header().Get("Vary"); got != "Origin" {
		t.Errorf("want Vary: Origin, got %q", got)
	}
}

func TestCORS_DisallowedOrigin_NoCORSHeaders(t *testing.T) {
	mw := cors([]string{"https://parkingfree.io"})
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

	req := httptest.NewRequest("GET", "/allowed", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("disallowed origin should not get CORS headers; got %q", got)
	}
}

func TestCORS_Preflight_AllowedOrigin_204(t *testing.T) {
	mw := cors([]string{"https://parkingfree.io"})
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Errorf("inner handler should not run on preflight")
	}))

	req := httptest.NewRequest("OPTIONS", "/allowed", nil)
	req.Header.Set("Origin", "https://parkingfree.io")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("want 204 on preflight, got %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Errorf("preflight should set Access-Control-Allow-Methods")
	}
}

func TestCORS_Preflight_DisallowedOrigin_403(t *testing.T) {
	mw := cors([]string{"https://parkingfree.io"})
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

	req := httptest.NewRequest("OPTIONS", "/allowed", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("want 403 on preflight from disallowed origin, got %d", rr.Code)
	}
}

func TestCORS_Wildcard_EchoesOrigin(t *testing.T) {
	mw := cors([]string{"*"})
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

	req := httptest.NewRequest("GET", "/allowed", nil)
	req.Header.Set("Origin", "https://anywhere.example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://anywhere.example.com" {
		t.Errorf("wildcard should echo origin; got %q", got)
	}
}
