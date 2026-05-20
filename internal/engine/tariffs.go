package engine

import "github.com/wang-hantao/parking-free/internal/domain"

// TariffClass is a named pricing schedule. Stockholm's PARKING_RATE
// strings always start with "taxa N:" where N picks one of these
// classes; two streets with the same class have identical pricing.
//
// Class definitions live here in code (not in a database table)
// because:
//   - they change rarely and tend to be city-wide reform events
//   - keeping them under version control gives review + history
//   - this avoids needing zone geometry just to attach a tariff
//   - it sidesteps the "operator_zone vs city zone vs road segment"
//     attachment problem
//
// When the project gains a second city, this map likely becomes
// city-keyed (or moves to a sourced reference table). For Stockholm
// alone, a flat map is the right size.
type TariffClass struct {
	Code        string
	Description string
	Currency    string
	Windows     []TariffWindowSpec
}

// TariffWindowSpec is a recurring tariff window. Matches the engine's
// existing TimeWindow semantics (WeekdayMask | DayType + StartMin |
// EndMin in minutes-of-day), but carries a rate rather than belonging
// to a rule.
//
// Priority breaks ties when multiple windows match the same moment:
// the higher-priority window's rate is the active one. Stockholm's
// pattern is "specific window at priority 10, catch-all at priority 0
// for övrig tid".
type TariffWindowSpec struct {
	WeekdayMask int            // 0 = no day-of-week filter; bit 0=Sun..6=Sat
	DayType     domain.DayType // "" = no day-type filter
	StartMin    int            // minutes from midnight, inclusive
	EndMin      int            // exclusive (1440 == end of day)
	Rate        float64        // amount in Currency
	PerSec      int            // billing unit: 3600 = per hour
	Priority    int            // higher = more specific
}

// TariffClasses is the default registry. Evaluator copies a reference
// at construction; tests can override via WithTariffClasses.
var TariffClasses = map[string]TariffClass{
	// taxa 1: inner-city premium, flat 24/7.
	//   "55 kr/tim alla dagar 00-24"
	"stockholm.taxa.1": {
		Code:        "stockholm.taxa.1",
		Description: "Stockholm taxa 1 — 55 SEK/h, all days 00-24",
		Currency:    "SEK",
		Windows: []TariffWindowSpec{
			{WeekdayMask: 0, StartMin: 0, EndMin: 1440, Rate: 55, PerSec: 3600, Priority: 0},
		},
	},

	// taxa 2: mid-tier.
	//   "31 kr/tim vardagar 7-21 och dag före helgdag och helgdag 9-19,
	//    20 kr/tim övrig tid"
	"stockholm.taxa.2": {
		Code:        "stockholm.taxa.2",
		Description: "Stockholm taxa 2 — 31 SEK/h Mon-Fri 07-21 + pre-holiday/holiday 09-19; 20 SEK/h other times",
		Currency:    "SEK",
		Windows: []TariffWindowSpec{
			{DayType: domain.DayTypeNormal, StartMin: 420, EndMin: 1260, Rate: 31, PerSec: 3600, Priority: 10},
			{DayType: domain.DayTypePreHoliday, StartMin: 540, EndMin: 1140, Rate: 31, PerSec: 3600, Priority: 10},
			{DayType: domain.DayTypeHoliday, StartMin: 540, EndMin: 1140, Rate: 31, PerSec: 3600, Priority: 10},
			// "övrig tid" — catch-all at priority 0.
			{WeekdayMask: 0, StartMin: 0, EndMin: 1440, Rate: 20, PerSec: 3600, Priority: 0},
		},
	},

	// taxa 3: lower-tier residential streets.
	//   "20 kr/tim vardagar 7-19, 15 kr/tim dag före helgdag 11-17"
	// No catch-all rate — outside these windows parking is free.
	"stockholm.taxa.3": {
		Code:        "stockholm.taxa.3",
		Description: "Stockholm taxa 3 — 20 SEK/h Mon-Fri 07-19; 15 SEK/h pre-holiday 11-17; free other times",
		Currency:    "SEK",
		Windows: []TariffWindowSpec{
			{DayType: domain.DayTypeNormal, StartMin: 420, EndMin: 1140, Rate: 20, PerSec: 3600, Priority: 10},
			{DayType: domain.DayTypePreHoliday, StartMin: 660, EndMin: 1020, Rate: 15, PerSec: 3600, Priority: 10},
		},
	},

	// taxa 12: MC reduced (mid-tier).
	//   "7,75 kr/tim vardagar 07-21 + pre-hol/holiday 9-19, 5 kr/tim övrig tid"
	"stockholm.taxa.12": {
		Code:        "stockholm.taxa.12",
		Description: "Stockholm taxa 12 (MC) — 7.75 SEK/h Mon-Fri 07-21 + pre-holiday/holiday 09-19; 5 SEK/h other",
		Currency:    "SEK",
		Windows: []TariffWindowSpec{
			{DayType: domain.DayTypeNormal, StartMin: 420, EndMin: 1260, Rate: 7.75, PerSec: 3600, Priority: 10},
			{DayType: domain.DayTypePreHoliday, StartMin: 540, EndMin: 1140, Rate: 7.75, PerSec: 3600, Priority: 10},
			{DayType: domain.DayTypeHoliday, StartMin: 540, EndMin: 1140, Rate: 7.75, PerSec: 3600, Priority: 10},
			{WeekdayMask: 0, StartMin: 0, EndMin: 1440, Rate: 5, PerSec: 3600, Priority: 0},
		},
	},

	// taxa 13: MC reduced (lower-tier).
	//   "5 kr/tim vardagar 7-19; 3,75 kr/tim dag före helgdag 11-17"
	"stockholm.taxa.13": {
		Code:        "stockholm.taxa.13",
		Description: "Stockholm taxa 13 (MC) — 5 SEK/h Mon-Fri 07-19; 3.75 SEK/h pre-holiday 11-17; free other times",
		Currency:    "SEK",
		Windows: []TariffWindowSpec{
			{DayType: domain.DayTypeNormal, StartMin: 420, EndMin: 1140, Rate: 5, PerSec: 3600, Priority: 10},
			{DayType: domain.DayTypePreHoliday, StartMin: 660, EndMin: 1020, Rate: 3.75, PerSec: 3600, Priority: 10},
		},
	},
}
