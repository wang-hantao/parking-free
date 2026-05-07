// Package domain holds the core types of the parking-rules platform.
// Nothing in this package should depend on any other package in the
// repository — it is the universal language across adapters, store, and
// engine.
package domain

import "math"

// Coordinate is a WGS84 latitude/longitude pair.
//
// Although the source data (NVDB, LTF-Tolken in default config) uses
// SWEREF99 TM, all values are normalised to WGS84 at the adapter layer
// so that the engine and HTTP API speak in lat/lng.
type Coordinate struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// DistanceMeters returns the great-circle distance between two
// coordinates using the Haversine formula. Accurate to within a few
// metres at city scale, which is sufficient for the 10m-rule and
// geofence radius checks.
func (c Coordinate) DistanceMeters(other Coordinate) float64 {
	const earthRadiusM = 6371000.0
	lat1 := c.Lat * math.Pi / 180
	lat2 := other.Lat * math.Pi / 180
	dLat := lat2 - lat1
	dLng := (other.Lng - c.Lng) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLng/2)*math.Sin(dLng/2)
	c2 := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusM * c2
}

// VehicleClass represents the categorical kind of a vehicle. Used by
// rules that only apply to certain classes (bus-only spots, etc.).
type VehicleClass string

const (
	VehicleCar        VehicleClass = "car"
	VehicleMotorcycle VehicleClass = "motorcycle"
	VehicleBus        VehicleClass = "bus"
	VehicleTruck      VehicleClass = "truck"
	VehicleEV         VehicleClass = "ev"
)

// Vehicle is the subject of a parking-permission query.
type Vehicle struct {
	Plate   string       `json:"plate"`
	Country string       `json:"country"` // ISO 3166-1 alpha-2 (e.g. "SE")
	Class   VehicleClass `json:"class"`
}
