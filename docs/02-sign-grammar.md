# 02 — Sign Grammar (How to Read Swedish Parking Signs)

This is the most concentrated source of confusion, the most-cited cause
of fines, and the place where a software interpreter has the highest
leverage. Get this right and you eliminate most "I didn't understand
the sign" tickets.

## The three-colour time grammar

Supplementary plates under parking signs show times in three colours.
Each colour indicates which type of day the time window applies to.

| Colour | Day type | Typical interpretation |
|--------|----------|------------------------|
| Black or white, **no brackets** | Normal weekdays | Mon–Fri (excluding pre-holidays) |
| Black or white, **in brackets** | Day before Sun/holiday | Usually Saturdays, but also any working day immediately before a red day |
| **Red** | Sundays and public holidays | "Red days" — Sundays + Swedish public holidays |

### Critical edge case (often misunderstood)

Brackets do **not** simply mean "Saturday." They mean "the day before a
red day." So:
- Most Saturdays → bracket times apply (because Sunday follows)
- A Saturday that is itself a holiday → red times apply on Saturday,
  AND the Friday before gets bracket times (because Saturday is now a
  red day)
- Christmas Eve, Midsummer Eve, New Year's Eve → typically bracket times
  if the next day is a red day; red times if they are red days
  themselves under the relevant calendar

The Swedish "Red days" calendar (helgdagar):
- New Year's Day
- Epiphany (Trettondedag jul) — 6 January
- Good Friday, Easter Saturday, Easter Sunday, Easter Monday
- May 1 (Labour Day)
- Ascension Day (Kristi himmelfärdsdag) — 6th Thursday after Easter
- Pentecost (Pingstdagen) — 7th Sunday after Easter
- National Day — 6 June
- Midsummer Day (Midsommardagen) — Saturday between 20–26 June
- All Saints' Day (Alla helgons dag) — Saturday between 31 Oct – 6 Nov
- Christmas Day, Second Day of Christmas, New Year's Eve (custom)
- All Sundays

For an implementation: maintain a `HolidayCalendar(country, region, date)`
table populated yearly. Day-type for any given date is derived from this
table. Stockholm-region-specific holidays do not currently exist but
the model should allow it.

## Examples of how to read a sign

```
P-skylt
9-18         <- normal weekdays Mon-Fri, parking allowed 09:00-18:00
(8-14)       <- day before red day (e.g. Saturday), 08:00-14:00
8-12         <- red days (Sundays/holidays), 08:00-12:00
2 tim        <- max 2 hour stay during the above windows
```

Reading: Mon–Fri you can park 9–18, max 2h. On Saturday before a
Sunday, 8–14, max 2h. On Sundays and holidays, 8–12, max 2h.
Outside these windows, parking is **free** (no payment required) but
all other rules still apply (24h rule, etc.).

## Charge sign vs. permission sign

Two visually similar but legally distinct signs:
- **P** with time → parking is **permitted** during these times.
  Outside, it may be permitted but subject to other rules, OR forbidden.
- **Avgift** + times → during these times, payment is required.
  Outside, parking may be free but still permitted.

Always read both the main sign and any supplementary plates. Many
tickets come from drivers who saw "P 9-18" and assumed parking was
forbidden outside those hours — usually it is permitted, just unpaid
or with different rules.

## Servicedag (street cleaning)

Stockholm operates weekly cleaning days. These appear on signs as a
separate plate:
- Inner city: usually one weekday between **00:00–06:00**
- Outer areas: usually one weekday between **08:00–16:00**

Vehicles must be moved **before** the start of the cleaning window. A
fine is issued if the vehicle is present at any point during the
window. Even a fully-paid parking session does not exempt you from the
servicedag rule — they are independent.

This is exposed as a dedicated dataset in LTF-Tolken:
`/v1/servicedagar/...` — see file 04.

## P-skiva (parking disc)

Used in some marked areas as an alternative to paid parking.
- Set the disc to the **next half-hour after arrival** (e.g. arrive
  13:02 → set to 13:30; arrive 14:40 → set to 15:00).
- Disc must be clearly visible inside the windshield.
- Free to obtain (most Swedish supermarkets give them away).
- Some Stockholm zones require an app instead of a disc — check
  the sign carefully. The disc-vs-app distinction is not always obvious.

## Boendeparkering (residential permit)

A "Boendeparkering" sign means residents of that area with a paid
permit have priority. Without a permit:
- You can usually still park, but pay the visitor rate (which is
  often higher and time-limited).
- The permit, if you have one, must be tied to your registration
  plate in the parking-app system or visibly displayed.

In Stockholm, residential permits are zone-bound — a permit for zone X
does not work in zone Y. Zones are not the same as administrative
districts; they are defined by Trafikkontoret as separate polygons
exposed in the WMS/WFS layer `Boendeparkeringsområden`.

## Direction-of-validity arrows

A small arrow on the sign indicates the direction in which the rule
applies along the street. A sign with no arrow at the start of a block
means "the rule applies from here onward in the parking direction."
This is a common source of confusion when a sign is mid-block — the
rules behind the sign may differ from the rules in front of it.

## Heuristics for an interpreter implementation

When parsing a sign image to extract rules:

1. **Identify the main symbol**: P (parking allowed), No-P (no parking),
   No-Stop (no stopping), P-skiva (disc), Avgift (charge), Boende
   (resident), various vehicle-class restrictions.
2. **Extract supplementary plate text** by colour:
   - White/black non-bracketed → weekday rule
   - White/black bracketed → pre-holiday rule
   - Red → holiday rule
3. **Parse time ranges** in HH-HH or HH:MM-HH:MM format.
4. **Extract max-duration** ("2 tim", "30 min", "24 tim").
5. **Extract direction arrows** if present.
6. **Cross-reference location** against the LTF-Tolken `within` query
   to validate that the sign's interpretation matches the published
   föreskrift. If there is a mismatch, the sign wins for the user
   (because it is what's enforced) but a warning should be logged.

## Sources

- Trafiko (Sweden) — Stopping & Parking, supplementary plates:
  <https://trafiko.se/en/faktabank/stannande-parkering>
- The Local — How to avoid parking fines in Sweden (good plain-English
  explanation including bracket/red day clarification):
  <https://www.thelocal.se/20200911/how-to-avoid-getting-too-many-parking-fines-in-sweden>
- Korkortonline — Stop & park theory:
  <https://korkortonline.se/en/theory/stop-park/>
