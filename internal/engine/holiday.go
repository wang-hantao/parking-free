// Package engine evaluates a parking query against the regulation
// graph and returns a Verdict. It is the kernel of the platform — no
// I/O, no HTTP, no SQL. All required state is provided by the caller.
package engine

import (
	"time"

	"github.com/wang-hantao/parking-free/internal/domain"
)

// HolidayCalendar resolves dates to DayTypes. This is the lynchpin of
// correctly interpreting Swedish parking signs: the bracket vs red
// vs white grammar collapses to "what DayType does today fall under?"
type HolidayCalendar struct {
	holidays map[civilDate]bool // explicit fixed-date entries
	region   string             // e.g. "SE" — for future regional variants
}

type civilDate struct{ year, month, day int }

func keyOf(t time.Time) civilDate {
	y, m, d := t.Date()
	return civilDate{y, int(m), d}
}

// NewHolidayCalendarSE returns a calendar pre-populated with Swedish
// public holidays. The set of fixed-date holidays is added per-year
// on first lookup; movable feasts (Easter etc.) are computed.
func NewHolidayCalendarSE() *HolidayCalendar {
	return &HolidayCalendar{
		holidays: make(map[civilDate]bool),
		region:   "SE",
	}
}

// AddHoliday registers an explicit calendar date as a holiday.
// Useful for tests and for any region-specific entries not covered
// by the built-in Swedish set.
func (h *HolidayCalendar) AddHoliday(t time.Time) {
	h.holidays[keyOf(t)] = true
}

// IsHoliday reports whether the given date is a Swedish public
// holiday (a "red day").
func (h *HolidayCalendar) IsHoliday(t time.Time) bool {
	if h.holidays[keyOf(t)] {
		return true
	}
	// Swedish fixed-date holidays
	y := t.Year()
	month, day := int(t.Month()), t.Day()
	switch {
	case month == 1 && day == 1: // Nyårsdagen
		return true
	case month == 1 && day == 6: // Trettondedag jul
		return true
	case month == 5 && day == 1: // Första maj
		return true
	case month == 6 && day == 6: // Nationaldagen
		return true
	case month == 12 && day == 25: // Juldagen
		return true
	case month == 12 && day == 26: // Annandag jul
		return true
	}
	// Easter-relative movable feasts
	easter := easterSunday(y)
	for _, off := range []int{-2, 0, 1, 39, 49} { // Good Fri, Easter Sun, Easter Mon, Ascension, Pentecost
		if sameDate(t, easter.AddDate(0, 0, off)) {
			return true
		}
	}
	// Midsummer Day: Saturday between 20–26 June
	if month == 6 && day >= 20 && day <= 26 && t.Weekday() == time.Saturday {
		return true
	}
	// All Saints' Day: Saturday between 31 Oct – 6 Nov
	if (month == 10 && day == 31) || (month == 11 && day >= 1 && day <= 6) {
		if t.Weekday() == time.Saturday {
			return true
		}
	}
	return false
}

// DayType returns the DayType applicable to a given moment.
//
//   - DayTypeHoliday: Sundays and public holidays
//   - DayTypePreHoliday: the day before a holiday (incl. day before
//     Sunday, which is most Saturdays)
//   - DayTypeNormal: everything else
func (h *HolidayCalendar) DayType(t time.Time) domain.DayType {
	if t.Weekday() == time.Sunday || h.IsHoliday(t) {
		return domain.DayTypeHoliday
	}
	tomorrow := t.AddDate(0, 0, 1)
	if tomorrow.Weekday() == time.Sunday || h.IsHoliday(tomorrow) {
		return domain.DayTypePreHoliday
	}
	return domain.DayTypeNormal
}

func sameDate(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// easterSunday returns the date of Easter Sunday for the given year
// using the anonymous Gregorian algorithm. Time is local midnight; the
// caller should add offsets to compute Good Friday, Ascension, etc.
func easterSunday(year int) time.Time {
	a := year % 19
	b := year / 100
	c := year % 100
	d := b / 4
	e := b % 4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	hh := (19*a + b - d - g + 15) % 30
	i := c / 4
	k := c % 4
	l := (32 + 2*e + 2*i - hh - k) % 7
	m := (a + 11*hh + 22*l) / 451
	month := (hh + l - 7*m + 114) / 31
	day := ((hh + l - 7*m + 114) % 31) + 1
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}
