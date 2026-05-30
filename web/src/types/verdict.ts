// Type definitions mirroring the Go backend's domain types.
//
// Keep these in lockstep with internal/domain/*.go on the server.
// When the server adds or renames a field, update the type here as
// part of the same PR — there's no codegen yet, so drift only shows
// up as a TypeScript error when a component tries to access a field
// that no longer exists, or as silent missing data when a field is
// renamed but the old name is still queried.

export type RuleKind = "allow" | "forbid" | "restrict";
export type VehicleClass = "car" | "motorcycle" | "truck" | "bus";
export type PermitKind =
  | "residential"
  | "disabled"
  | "electric"
  | "carpool"
  | "guest"
  | "nytto_a"
  | "nytto_b";
export type DayType = "normal" | "pre_holiday" | "holiday";
export type WarningKind =
  | "servicedag_upcoming"
  | "max_stay_expiring"
  | "permit_expiring_soon"
  | "tariff_change"
  | "weather_restriction"
  | "event_restriction";
export type NeedsAction = "pay_via_app" | "obtain_permit" | "show_disc" | string;

// --- Sub-types -------------------------------------------------------------

export interface Source {
  system: string;
  reference: string;
}

export interface Reason {
  rule_id: string;
  regulation_id: string;
  source: Source;
  disposition: RuleKind;
  human_readable: string;
  supports: boolean;
  blocks?: boolean;
  // Set when a more-specific Allow rule at the same location
  // overrides this one (e.g. a disabled bay carving into a general
  // paid-parking strip). Surface in UI as informational context, not
  // as a binding rule.
  superseded?: boolean;
}

export interface ZoneRef {
  id: string;
  code: string;
  city: string;
  kind: string;
}

export interface LocationInfo {
  zone?: ZoneRef;
  street?: string;
  municipality?: string;
}

export interface Rate {
  amount: number;
  per: "hour" | "minute" | "day";
}

export interface OperatorOption {
  id: string;
  name: string;
  // Operator-specific zone code (e.g. EasyPark "5012"). Empty when
  // the verdict's location couldn't be resolved to a known zone —
  // the user types the area code from the on-street sign instead.
  external_zone_id?: string;
  // URL to launch the operator's app or web payment flow. For
  // Stockholm's four operators this is a landing URL (e.g.
  // https://web.easypark.net/) — Android/iOS universal-link
  // handlers route into the installed app when available, browser
  // otherwise.
  deeplink?: string;
}

export interface PricingInfo {
  currency?: string;
  is_free_now?: boolean;
  current_rate?: Rate;
  next_rate_change?: string; // RFC3339 timestamp
  next_rate?: Rate;
  max_session_cost?: number;
  operators?: OperatorOption[];
}

export interface Constraints {
  max_stay_minutes?: number;
  payment_required?: boolean;
  permit_required?: boolean;
  vehicle_classes?: VehicleClass[];
}

export interface Warning {
  kind: WarningKind;
  severity: "info" | "warning" | "danger";
  starts_at?: string;
  ends_at?: string;
  human_readable: string;
}

export interface CostSegment {
  from: string;
  to: string;
  rate: number;
  cost: number;
}

export interface CostEstimate {
  duration_minutes: number;
  currency: string;
  total: number;
  breakdown: CostSegment[];
}

export interface Metadata {
  evaluated_at: string;
  engine_version: string;
  mode?: "" | "strict";
}

// --- The verdict -----------------------------------------------------------

export interface Verdict {
  allowed: boolean;
  summary?: string;
  expires_at: string;
  reasons: Reason[];
  needs_action?: NeedsAction[];
  location?: LocationInfo;
  pricing?: PricingInfo;
  constraints?: Constraints;
  warnings?: Warning[];
  estimated_cost?: CostEstimate;
  metadata?: Metadata;
}

// --- Query input -----------------------------------------------------------

export interface VerdictQuery {
  lat: number;
  lng: number;
  plate: string;
  class?: VehicleClass;
  at?: string;                // RFC3339; omit for "now"
  radius?: number;            // metres; omit for default 50
  duration_minutes?: number;  // if set, server returns estimated_cost
  mode?: "nearby" | "strict";
}
