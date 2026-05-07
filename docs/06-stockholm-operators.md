# 06 — Stockholm Operators and the Authorization System

The commercial layer that sits on top of the regulation graph. This is
where pricing, payment, and the customer relationship live — and where
much of the daily fine production happens (wrong app, wrong zone, wrong
plate).

## Stockholm's authorization system

Stockholm operates a mandated multi-provider model that should look
familiar from MVNO/MVNE economics. The city ("network operator")
authorizes a set of payment providers ("MVNOs") and requires them to
all offer the same underlying tariff at the same price.

Key rules:
- Stockholm currently authorizes **four** providers: **EasyPark,
  Parkster, Mobill, ePARK**.
- All providers must offer payment for street parking in zones 1–30,
  including residential parking.
- All providers must offer the city's tariff with **no service fee
  beyond the parking rate** for the basic offering. Providers can
  charge service fees on premium tiers (push notifications, EV
  integration, etc.) but the base must be free.
- The city provides this list publicly so users can choose.

This means the consumer experience differs only on UX, not on
underlying price. Differentiation is software-only — the same
structural reality as MVNO retail competition where wholesale rates
are regulated.

The previous in-house city app, **Betala P**, was discontinued
**1 October 2024** — the city stepped out of direct provision and
left the four authorized operators to compete.

Source: EasyPark Stockholm guide; Stockholms stad parking pages.

## The four operators

| Operator | App / Web | Notes |
|----------|-----------|-------|
| **EasyPark** | EasyPark Go (49 SEK/month flat) and EasyPark Small (15% service fee per session, min 4.95 SEK); free mobile-friendly web app for Stockholm city zones with no service fee | International (20+ countries). Strongest brand recognition. The free Stockholm web app is a direct response to the authorization-system requirement. |
| **Parkster** | Parkster app | Strong in private-land parking and smaller cities. SMS-based fallback. |
| **Mobill** (was "Mobilpark") | Mobill app | Common in Stockholm suburbs and for fleet/business accounts. |
| **ePARK** | ePARK app | Newer authorized provider; less ubiquitous but compliant with the same fee-parity rules. |

All four implement broadly similar product flows: register a vehicle
plate → enter or auto-detect zone code → start session → app charges
periodically until you stop. The differentiation comes from:
- Web-vs-native fallback (EasyPark has the strongest free web app)
- EV-charging integration
- Notification and auto-extend behaviour
- Receipts, expense tracking, fleet features

## Parking-app failure modes (most common ticket causes)

These are documented in user reviews and Police FAQs:

1. **Wrong plate typed** — O vs 0, B vs 8, etc. The Police explicitly
   will not waive a fine for this. Mitigation product: OCR auto-fill
   from a photo of the plate.
2. **Wrong zone selected** — many zones are visually adjacent on a
   map but separately tariffed. Mitigation: GPS-based zone detection
   with a confirmation screen.
3. **Wrong app for the area** — paying via Operator X for an area
   that actually requires a private-land app. The Police also will
   not waive this. Mitigation: detect public-vs-private at the
   geofence level.
4. **Inactive permit** — residential permit lapsed, or vehicle plate
   not on the permit anymore.
5. **Permit not visible** — for paper permits / parking discs.
   The Police will not waive even if you can prove afterwards that
   the permit was valid. Solely a problem for the disc/paper case;
   digital permits don't have this issue.
6. **Forgot to extend** — most apps notify but users miss it.
   Mitigation: auto-extend with a hard cap.

## Permit types

Stockholm offers several permit categories. The relevant ones for
software:

| Permit | Eligibility | Use case |
|--------|-------------|----------|
| **Boendeparkering** (Residential) | Registered as resident in the zone | Park at lower rates, longer durations, in your zone |
| **Nyttoparkeringstillstånd A** | Tradespeople, deliveries (broad use) | 24/7 across the city, 25 000 SEK/year (2024) |
| **Nyttoparkeringstillstånd B** | Tradespeople (limited hours) | 07:00–19:00 across the city, 18 750 SEK/year (2024) |
| **Rörelsehindrad** (Disabled) | Medical certificate | Disabled spots and extended free parking on regular spots |
| **Carpool** | Registered carpool vehicle | Designated carpool spots |

Pricing as of 1 Feb 2024 (see file 03 for residential pricing).

## Private parking operators (Kontrollavgift regime)

Distinct from the four authorized municipal operators. These manage
private land (BRFs, shopping centres, business parks, hotels):

- **Aimo Park** (was Q-Park Sweden, then Sunfleet's Aimo) — large
  garage operator across Sweden
- **Apcoa Parking** — major European operator
- **Parkit** — focused on BRFs and private property managers
- **Stockholm Parkering AB** — the city-owned operator that runs
  many municipal garages (legally still operates under
  Kontrollavgift on private land it leases)
- Plus a long tail of smaller landlords/companies issuing their own
  white tickets via subcontractors

Implication for product design: a parking-permission query for a
random Stockholm coordinate may need to consult **multiple data
sources**:
1. LTF-Tolken (municipal street parking)
2. Operator deeplink databases (which app for this address)
3. Private-land contracts (impossible to enumerate centrally; this
   is a real coverage gap and a B2B opportunity — see README intro)

## EV charging and parking interaction

EV charging spots (`Laddplats`) are their own regulation class:
- **Parking on a laddplats without charging is a fineable offence.**
- **Charging payment and parking payment are separate.** The
  charging operator (Vattenfall InCharge, Mer, Ionity, etc.)
  collects the charging fee; you must still pay the parking fee
  (typically zero on a laddplats, but check signage) via one of
  the four authorized parking apps.
- Some apps (notably EasyPark) integrate with charging APIs so the
  user starts both with one tap; this is not the default behaviour
  in all situations.

Trafikkontoret exposes laddplats geometries via the standard WFS
endpoint (see file 05).

## Implications for a platform design

If your product is a consumer app:
- Sit **above** the four operators rather than competing with them.
  Either deeplink into them or use their (unofficial) APIs to start
  sessions on the user's behalf.
- The differentiator is the regulation-graph kernel, not the payment
  rails. The operators handle payment; you handle "is this legal,
  is this smart."

If your product is B2B:
- The BRF / property-manager segment is underserved on the
  Kontrollavgift side. Tools for issuing digital permits, tracking
  guest passes, automating disputes — this is the SaaS layer that
  doesn't exist cleanly today.
- A "white-label parking-as-a-service" platform plays here, with
  EasyPark/Parkster as one optional payment integration among many.

## Sources

- EasyPark — Parking apps in Stockholm:
  <https://www.easypark.com/en-se/where-to-park/parking-apps-in-stockholm>
- EasyPark — Parking in Stockholm guide:
  <https://www.easypark.com/en-se/where-to-park/parking-in-stockholm>
- sthlmparkering.se — operator switch references the four authorized:
  <https://www.sthlmparkering.se/help>
- Stockholms stad — Boendeparkering / Nyttoparkering pricing
  decision:
  <https://press.newsmachine.com/pressrelease/view/stockholm-justerar-parkeringsavgifter-40342>
- Parkit — Yellow vs white ticket explainer:
  <https://www.parkit.se/en/parkit-explains/parking-fine>
