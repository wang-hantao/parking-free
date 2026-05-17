package domain

// Geometry-bearing records used at ingest time. These types carry
// optional inline WKT so external sources (LTF-Tolken features, NVDB,
// etc.) can be upserted into the geometry tables in one shot.
//
// On read paths the Store doesn't currently hydrate geometry into Go
// (it stays inside Postgres for spatial joins), so these structs are
// write-focused. WKT must be in WGS84 (SRID 4326).

// RoadSegment is a linear road geometry, source-keyed for idempotent
// upsert. GeometryWKT is a LineString such as
// "LINESTRING(18.10 59.33, 18.11 59.34)".
type RoadSegment struct {
	ID           string
	Source       Source
	StreetName   string
	Municipality string
	Direction    string // "forward" | "reverse" | "both" — optional
	GeometryWKT  string
}
