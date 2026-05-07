package engine

import (
	"testing"
	"time"

	"github.com/wang-hantao/parking-free/internal/domain"
)

func date(y, m, d int) time.Time {
	return time.Date(y, time.Month(m), d, 12, 0, 0, 0, time.UTC)
}

func TestHolidayCalendar_FixedHolidays(t *testing.T) {
	cal := NewHolidayCalendarSE()
	cases := []struct {
		name string
		date time.Time
	}{
		{"New Year's Day", date(2026, 1, 1)},
		{"Epiphany", date(2026, 1, 6)},
		{"May Day", date(2026, 5, 1)},
		{"National Day", date(2026, 6, 6)},
		{"Christmas Day", date(2026, 12, 25)},
		{"Boxing Day", date(2026, 12, 26)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !cal.IsHoliday(tc.date) {
				t.Errorf("expected %s to be a holiday", tc.date.Format("2006-01-02"))
			}
		})
	}
}

func TestHolidayCalendar_NonHolidays(t *testing.T) {
	cal := NewHolidayCalendarSE()
	// IsHoliday does not flag plain Sundays — Sundays are handled by
	// DayType, not by IsHoliday. So an arbitrary mid-month Tuesday
	// must report false.
	d := date(2026, 3, 17)
	if cal.IsHoliday(d) {
		t.Errorf("Tuesday %s should not be a holiday", d.Format("2006-01-02"))
	}
}

func TestHolidayCalendar_EasterRelated(t *testing.T) {
	cal := NewHolidayCalendarSE()
	easter := date(2026, 4, 5)
	cases := []struct {
		name   string
		offset int
	}{
		{"Good Friday", -2},
		{"Easter Sunday", 0},
		{"Easter Monday", 1},
		{"Ascension Day", 39},
		{"Pentecost Sunday", 49},
	}
	for _, tc := range cases {
		d := easter.AddDate(0, 0, tc.offset)
		if !cal.IsHoliday(d) {
			t.Errorf("%s (%s) should be a holiday", tc.name, d.Format("2006-01-02"))
		}
	}
}

func TestHolidayCalendar_MidsummerAndAllSaints(t *testing.T) {
	cal := NewHolidayCalendarSE()
	midsummer := date(2026, 6, 20)
	if midsummer.Weekday() != time.Saturday {
		t.Fatalf("setup: expected Saturday, got %v", midsummer.Weekday())
	}
	if !cal.IsHoliday(midsummer) {
		t.Errorf("Midsummer Day should be a holiday")
	}

	allSaints := date(2026, 10, 31)
	if allSaints.Weekday() != time.Saturday {
		t.Fatalf("setup: expected Saturday, got %v", allSaints.Weekday())
	}
	if !cal.IsHoliday(allSaints) {
		t.Errorf("All Saints' Day should be a holiday")
	}
}

func TestHolidayCalendar_DayType_Normal(t *testing.T) {
	cal := NewHolidayCalendarSE()
	tue := date(2026, 3, 17)
	if got := cal.DayType(tue); got != domain.DayTypeNormal {
		t.Errorf("Tuesday DayType: want %q, got %q", domain.DayTypeNormal, got)
	}
}

func TestHolidayCalendar_DayType_PreHoliday_Saturday(t *testing.T) {
	cal := NewHolidayCalendarSE()
	sat := date(2026, 3, 14)
	if got := cal.DayType(sat); got != domain.DayTypePreHoliday {
		t.Errorf("Saturday DayType: want %q, got %q", domain.DayTypePreHoliday, got)
	}
}

func TestHolidayCalendar_DayType_PreHoliday_BeforeFixedHoliday(t *testing.T) {
	cal := NewHolidayCalendarSE()
	dec24 := date(2026, 12, 24)
	if got := cal.DayType(dec24); got != domain.DayTypePreHoliday {
		t.Errorf("Christmas Eve DayType: want %q, got %q", domain.DayTypePreHoliday, got)
	}
}

func TestHolidayCalendar_DayType_Holiday_Sunday(t *testing.T) {
	cal := NewHolidayCalendarSE()
	sun := date(2026, 3, 15)
	if got := cal.DayType(sun); got != domain.DayTypeHoliday {
		t.Errorf("Sunday DayType: want %q, got %q", domain.DayTypeHoliday, got)
	}
}

func TestHolidayCalendar_DayType_Holiday_FixedDate(t *testing.T) {
	cal := NewHolidayCalendarSE()
	may1 := date(2026, 5, 1)
	if got := cal.DayType(may1); got != domain.DayTypeHoliday {
		t.Errorf("May 1 DayType: want %q, got %q", domain.DayTypeHoliday, got)
	}
}

func TestHolidayCalendar_DayType_PreHoliday_BeforeMovableHoliday(t *testing.T) {
	cal := NewHolidayCalendarSE()
	maundyThursday := date(2026, 4, 2)
	if got := cal.DayType(maundyThursday); got != domain.DayTypePreHoliday {
		t.Errorf("Maundy Thursday DayType: want %q, got %q", domain.DayTypePreHoliday, got)
	}
}

func TestEasterSunday(t *testing.T) {
	cases := map[int]string{
		2024: "2024-03-31",
		2025: "2025-04-20",
		2026: "2026-04-05",
		2027: "2027-03-28",
	}
	for year, expected := range cases {
		got := easterSunday(year).Format("2006-01-02")
		if got != expected {
			t.Errorf("Easter %d: want %s, got %s", year, expected, got)
		}
	}
}
