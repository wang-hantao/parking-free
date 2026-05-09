package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/wang-hantao/parking-free/internal/domain"
	"github.com/wang-hantao/parking-free/internal/engine"
)

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAllowed answers "is parking allowed here right now?".
//
// Query parameters:
//
//	lat              (required) — latitude in WGS84
//	lng              (required) — longitude in WGS84
//	plate            (required) — vehicle registration
//	class            (optional) — vehicle class, default "car"
//	at               (optional) — RFC3339 timestamp, defaults to server now
//	radius           (optional) — search radius in metres, default 50
//	duration_minutes (optional) — desired stay; if provided, the
//	                              response includes estimated_cost
func (s *Server) handleAllowed(w http.ResponseWriter, r *http.Request) {
	q, err := parseAllowedQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := s.engine.Evaluate(r.Context(), q)
	if err != nil {
		s.logger.Error("evaluate failed", "err", err)
		writeError(w, http.StatusInternalServerError, "evaluation failed")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func parseAllowedQuery(r *http.Request) (engine.Query, error) {
	v := r.URL.Query()

	latStr, lngStr, plate := v.Get("lat"), v.Get("lng"), v.Get("plate")
	if latStr == "" || lngStr == "" || plate == "" {
		return engine.Query{}, errors.New("lat, lng, and plate are required")
	}
	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		return engine.Query{}, errors.New("invalid lat")
	}
	lng, err := strconv.ParseFloat(lngStr, 64)
	if err != nil {
		return engine.Query{}, errors.New("invalid lng")
	}

	class := domain.VehicleClass(v.Get("class"))
	if class == "" {
		class = domain.VehicleCar
	}

	at := time.Now().UTC()
	if s := v.Get("at"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return engine.Query{}, errors.New("invalid at; expected RFC3339")
		}
		at = t
	}

	radius := 0.0
	if s := v.Get("radius"); s != "" {
		r, err := strconv.ParseFloat(s, 64)
		if err != nil || r < 0 {
			return engine.Query{}, errors.New("invalid radius")
		}
		radius = r
	}

	var duration time.Duration
	if s := v.Get("duration_minutes"); s != "" {
		m, err := strconv.Atoi(s)
		if err != nil || m < 0 {
			return engine.Query{}, errors.New("invalid duration_minutes")
		}
		duration = time.Duration(m) * time.Minute
	}

	return engine.Query{
		Position: domain.Coordinate{Lat: lat, Lng: lng},
		Vehicle:  domain.Vehicle{Plate: plate, Class: class},
		At:       at,
		RadiusM:  radius,
		Duration: duration,
	}, nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
