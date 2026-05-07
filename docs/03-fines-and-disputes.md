# 03 — Fines and Disputes

## The two regimes — yellow vs white

Two legally distinct parking-violation regimes exist in Sweden. They
look similar but are governed by different laws and have different
appeal paths.

| | **Yellow ticket** | **White ticket** |
|---|---|---|
| Swedish name | Parkeringsanmärkning | Kontrollavgift |
| Translation | "Parking notice" | "Control fee" |
| Land | Public (gatumark) | Private (tomtmark) |
| Legal basis | Lag (1976:206) om felparkeringsavgift | Lag (1984:318) om kontrollavgift |
| Issued by | Municipality / Police | Landowner or contracted parking company |
| Paid to | Transportstyrelsen via bankgiro 5051-6905 | The company stated on the ticket |
| Appeal to | Polismyndigheten | The issuing company |
| Strict liability | Yes — vehicle's registered owner must pay regardless | Yes |
| Pay-then-dispute | Required: must pay even while disputing; refund on success | Required: must pay even while disputing |

Identifying which regime: the ticket itself states the issuer. Yellow
tickets go to the Police, white tickets to the company. Mixing them
up is a common mistake — sending a private-land dispute to the Police
just gets it returned.

Source: Stockholms stad (parkering.stockholm); Polisen; Transportstyrelsen.

## National fine range

Per Förordning (1976:1128) 2a §, municipalities set their own fine
amounts within the federal range:

- **Minimum**: 75 SEK
- **Maximum**: 1 300 SEK

Within this range, municipalities differentiate by violation type and
optionally by area (inner city vs outer).

## Stockholm fine tiers (current)

Stockholm Kommunfullmäktige sets the local schedule. Current tiers:

| Tier | Amount (SEK) | Example violations |
|------|-------------|--------------------|
| Failed to pay | 900 | Did not pay on a paid-parking spot |
| Disrupting/obstructing | 1100 | Parked where parking was not permitted; overstayed time limit; parking ticket not visible; parking disc not set; wheel outside the parking marking; double-parked |
| Dangerous/blocking | 1300 | Stopped where stopping was forbidden; bus lane; bike or pedestrian path; emergency vehicle access |

Most-issued tier in Stockholm is 1300 SEK (over 30% of all fines).
The 900-tier is rare in practice.

The previous tier set (pre-2019) was 750 / 850 / 1200; raised by the
city in March 2019. Expect a similar revision pattern roughly every
5–7 years.

Source: SVT; sthlmparkering.se; Stockholms stad.

## Residential parking pricing (Boendeparkering)

For context — these are not fines but the cost of legal residential
parking, useful for product-pricing comparisons:

| Item | Price (per Feb 2024) |
|------|----------------------|
| Boendeparkering bil, taxa 2 & 3, monthly | 1 600 SEK |
| Boendeparkering bil, taxa 2 & 3, daily | 90 SEK |
| Boendeparkering MC, taxa 2 & 3, monthly | 400 SEK |
| Boendeparkering MC, taxa 2 & 3, daily | 22.50 SEK |
| Nyttoparkeringstillstånd A (24/7 city-wide) | 25 000 SEK/year |
| Nyttoparkeringstillstånd B (07–19, city-wide) | 18 750 SEK/year |

Source: Stockholms trafiknämnd, Oct 2023 decision; effective 1 Feb 2024.

## Payment and escalation timeline

| Day | Event | Amount |
|-----|-------|--------|
| 0 | Ticket issued; 8 days to pay | Original |
| 8 | Initial deadline | — |
| ~10 | First reminder ("erinran"), no surcharge | Original |
| ~31 | Second reminder ("åläggande"), with surcharge | Original + **150 SEK** |
| ~52 | Handed to Kronofogden (Enforcement Authority) | + collection fees |

Bankgiro for yellow ticket payment: **5051-6905**. The OCR/reference
number on the ticket must be used. Foreign-bank payments require the
SWIFT route documented at Transportstyrelsen — many foreign-plate
fines remain unpaid because of this friction.

Source: Transportstyrelsen.

## Who pays?

The vehicle's **registered owner** at the time of the violation has
strict liability. The driver's identity is irrelevant. If you sold the
car the morning of the violation but it was still in your name in the
Vägtrafikregistret, you owe the fine.

Exception: if circumstances make it likely the vehicle was taken from
the owner via a crime (theft).

Foreign-plate vehicles: the Police will attempt to collect across
borders, particularly within EU/EEA. Practical collection rates are
much lower than for domestic plates.

## Disputing a fine

### For yellow tickets (gatumark)

1. **Pay first.** Required regardless of dispute. Refunded on win.
2. Contact the **Police** — submit either a request for correction
   (`rättelse`, for clear errors like wrong make of car) or a contest
   (`bestridande`, for substantive disputes).
3. The Police review based on the parking attendant's notes and any
   evidence you submit. The 1-year limit applies: an `åläggande`
   cannot be issued more than one year after the violation date.
4. If the Police uphold the fine and you still disagree, the case
   proceeds to Tingsrätt (district court).

Reasons the Police explicitly will **not** waive a fine:
- You typed the wrong plate in the parking app
- You picked the wrong zone in the app
- You paid via the wrong operator's app for a municipal area
- Your permit was valid but not visible to the attendant
- The parking machine was broken (you must phone the listed
  out-of-service number; payment is still required somehow)

### For white tickets (tomtmark)

1. Pay or risk debt collection.
2. Dispute is sent to the **issuing company**, which adjudicates its
   own ticket — a known structural conflict of interest.
3. If declined, you can take the company to Tingsrätt as a civil claim.

## Bestridande letter — required information

For the Police (yellow) or company (white):
- Ärendenummer (case number from the ticket)
- Plate number
- Date, time, location
- Ground for dispute, with evidence (photos, GPS data, payment
  receipts)
- Claimant contact details, signature

A product feature opportunity: auto-generate this letter from a
parking session, with attached evidence, routing automatically based
on issuer.

## Stockholm fine volume (for sizing)

- Stockholm city issued ~430 000 tickets in 2021
- Stockholm county issued 258 884 tickets in H1 2024 alone
- Sweden-wide 2025: 1 344 147 tickets, 91% paid by year-end
- Stockholm leads the country; Malmö and Göteborg follow
- Total Sweden-wide payouts 2025: ~975 million SEK
- Transportstyrelsen retains 53 SEK per ticket; the rest goes to
  the issuing municipality

This is your TAM ceiling. ~1.3M tickets × ~1100 SEK average ≈ 1.4
billion SEK annual fine economy in Sweden alone.

Source: Transportstyrelsen press release (Feb 2026); Newsworthy.se
analysis of Transportstyrelsen database.

## Sources

- Stockholms stad — Parkeringsanmärkning:
  <https://parkering.stockholm/felparkering/parkeringsanmarkning/>
- Transportstyrelsen — Parkeringsanmärkning (process):
  <https://www.transportstyrelsen.se/sv/vagtrafik/fordon/skatter-och-avgifter/parkeringsanmarkning/>
- Polisen — Bestrida parkeringsanmärkning:
  <https://polisen.se/en/laws-and-regulations/fines/challenging-a-parking-fine/>
- sthlmparkering.se — Stockholm fine tiers reference:
  <https://www.sthlmparkering.se/help>
- SVT (2019) — Stockholm fine tier increase:
  <https://www.svt.se/nyheter/lokalt/stockholm/hojda-p-boter-i-stockholm-fran-den-1-april>
- Transportstyrelsen press release — 2025 statistics:
  <https://www.transportstyrelsen.se/sv/om-oss/pressrum/nyhetsarkiv/2026/flest-felparkerare-finns-i-stockholm/>
- Riksdagen — Förordning (1976:1128) 2a §:
  <https://www.riksdagen.se/sv/dokument-och-lagar/dokument/svensk-forfattningssamling/forordning-19761128-om-felparkeringsavgift_sfs-1976-1128/>
