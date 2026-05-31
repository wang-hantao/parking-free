// VerdictCard — renders a Verdict in a structured, glanceable way.
//
// Scaffolding version: shows the key fields (allowed, summary,
// pricing, location, needs_action) plus a collapsed JSON dump for
// debugging. Future iterations replace the dump with proper visual
// treatments for reasons, warnings, and estimated cost.

import { useState } from "react";
import type { NeedsAction, Verdict } from "../types/verdict";
import { PaymentLinks } from "./PaymentLinks";

interface VerdictCardProps {
  verdict: Verdict;
}

export function VerdictCard({ verdict }: VerdictCardProps) {
  // tone reflects the verdict outcome PLUS data quality. When we
  // have no rules at the location, we don't paint the card green —
  // the user needs to know they're operating on Sweden's legal
  // default, not on confirmed data.
  const tone: "allowed" | "forbidden" | "unknown" =
    verdict.data_confidence === "low"
      ? "unknown"
      : verdict.allowed
        ? "allowed"
        : "forbidden";
  return (
    <div className="rounded-xl bg-white p-5 shadow-sm ring-1 ring-slate-200">
      <Header
        allowed={verdict.allowed}
        summary={verdict.summary}
        tone={tone}
        confidence={verdict.data_confidence}
      />

      {verdict.location && (
        <div className="mt-3 text-sm text-slate-600">
          {verdict.location.street && (
            <p>
              <span className="text-slate-500">Street:</span> {verdict.location.street}
            </p>
          )}
          {verdict.location.zone && (
            <p>
              <span className="text-slate-500">Zone:</span>{" "}
              {verdict.location.zone.code} ({verdict.location.zone.kind})
            </p>
          )}
        </div>
      )}

      {verdict.pricing && <PricingBlock pricing={verdict.pricing} />}

      {verdict.constraints && <ConstraintsBlock constraints={verdict.constraints} />}

      {(() => {
        // pay_via_app becomes redundant text when we're about to
        // render operator buttons below. Strip it from the textual
        // needs_action list in that case; keep other actions (e.g.
        // obtain_permit) visible.
        const ops = verdict.pricing?.operators ?? [];
        const actions = (verdict.needs_action ?? []).filter(
          (a) => !(a === "pay_via_app" && ops.length > 0),
        );
        return (
          actions.length > 0 && <NeedsActionBlock actions={actions} />
        );
      })()}

      {verdict.pricing?.operators && verdict.pricing.operators.length > 0 && (
        <div className="mt-4 border-t border-slate-100 pt-4">
          <PaymentLinks operators={verdict.pricing.operators} />
        </div>
      )}

      {verdict.reasons && verdict.reasons.length > 0 && (
        <ReasonsBlock reasons={verdict.reasons} />
      )}

      <RawJson verdict={verdict} />
    </div>
  );
}

function Header({
  allowed,
  summary,
  tone,
  confidence,
}: {
  allowed: boolean;
  summary?: string;
  tone: "allowed" | "forbidden" | "unknown";
  confidence?: "high" | "low";
}) {
  const ring =
    tone === "allowed"
      ? "ring-allowed-500"
      : tone === "forbidden"
        ? "ring-forbidden-500"
        : "ring-slate-300";
  const bg =
    tone === "allowed"
      ? "bg-allowed-50"
      : tone === "forbidden"
        ? "bg-forbidden-50"
        : "bg-slate-50";
  const text =
    tone === "allowed"
      ? "text-allowed-700"
      : tone === "forbidden"
        ? "text-forbidden-700"
        : "text-slate-700";
  const dot =
    tone === "allowed"
      ? "bg-allowed-500"
      : tone === "forbidden"
        ? "bg-forbidden-500"
        : "bg-slate-400";
  const headline =
    tone === "unknown"
      ? "No data for this exact location"
      : allowed
        ? "Parking allowed"
        : "Parking not allowed";
  return (
    <div className={`flex items-start gap-3 rounded-lg ${bg} px-3 py-2 ring-1 ${ring}`}>
      <div className={`mt-0.5 h-2.5 w-2.5 flex-none rounded-full ${dot}`} aria-hidden />
      <div className="flex-1">
        <div className="flex items-center gap-2">
          <p className={`text-sm font-semibold ${text}`}>{headline}</p>
          {confidence === "low" && (
            <span className="rounded bg-slate-200 px-1.5 py-0.5 text-xs text-slate-600">
              low confidence
            </span>
          )}
        </div>
        {summary && <p className="mt-0.5 text-sm text-slate-700">{summary}</p>}
      </div>
    </div>
  );
}

function PricingBlock({ pricing }: { pricing: NonNullable<Verdict["pricing"]> }) {
  const fmt = (amount: number, per: string) =>
    `${amount.toLocaleString()} ${pricing.currency ?? ""}/${per}`;
  return (
    <div className="mt-4 text-sm">
      <p className="font-medium text-slate-700">Pricing</p>
      {pricing.is_free_now ? (
        <p className="mt-1 text-slate-600">Currently free.</p>
      ) : pricing.current_rate ? (
        <p className="mt-1 text-slate-600">
          {fmt(pricing.current_rate.amount, pricing.current_rate.per)}
        </p>
      ) : null}
      {pricing.next_rate_change && pricing.next_rate && (
        <p className="mt-0.5 text-xs text-slate-500">
          Changes to {fmt(pricing.next_rate.amount, pricing.next_rate.per)} at{" "}
          {new Date(pricing.next_rate_change).toLocaleString()}
        </p>
      )}
    </div>
  );
}

function ConstraintsBlock({
  constraints,
}: {
  constraints: NonNullable<Verdict["constraints"]>;
}) {
  return (
    <div className="mt-3 text-sm">
      <p className="font-medium text-slate-700">Constraints</p>
      <ul className="mt-1 space-y-0.5 text-slate-600">
        {constraints.max_stay_minutes && (
          <li>Max stay: {constraints.max_stay_minutes} min</li>
        )}
        {constraints.payment_required && <li>Payment required</li>}
        {constraints.permit_required && <li>Permit required</li>}
        {constraints.vehicle_classes && constraints.vehicle_classes.length > 0 && (
          <li>Reserved for: {constraints.vehicle_classes.join(", ")}</li>
        )}
      </ul>
    </div>
  );
}

function NeedsActionBlock({ actions }: { actions: NeedsAction[] }) {
  const label = (a: NeedsAction) => {
    switch (a) {
      case "pay_via_app":
        return "Pay via a parking app";
      case "obtain_permit":
        return "Obtain a permit";
      case "show_disc":
        return "Display a parking disc";
      default:
        return a;
    }
  };
  return (
    <div className="mt-3 text-sm">
      <p className="font-medium text-slate-700">What you need to do</p>
      <ul className="mt-1 list-disc pl-5 text-slate-600">
        {actions.map((a) => (
          <li key={a}>{label(a)}</li>
        ))}
      </ul>
    </div>
  );
}

function ReasonsBlock({ reasons }: { reasons: Verdict["reasons"] }) {
  return (
    <details className="mt-4">
      <summary className="cursor-pointer text-sm font-medium text-slate-700">
        Why ({reasons.length} {reasons.length === 1 ? "reason" : "reasons"})
      </summary>
      <ul className="mt-2 space-y-2 text-sm">
        {reasons.map((r) => (
          <li
            key={r.rule_id}
            className={`rounded border px-3 py-2 ${
              r.blocks
                ? "border-forbidden-500 bg-forbidden-50"
                : r.superseded
                ? "border-slate-200 bg-slate-50 opacity-60"
                : "border-slate-200 bg-slate-50"
            }`}
          >
            <p className="text-slate-700">
              {r.human_readable}
              {r.superseded && (
                <span className="ml-2 rounded bg-slate-200 px-1.5 py-0.5 text-xs text-slate-600">
                  superseded
                </span>
              )}
            </p>
            <p className="mt-1 text-xs text-slate-500">
              {r.source.system} / {r.source.reference}
            </p>
          </li>
        ))}
      </ul>
    </details>
  );
}

function RawJson({ verdict }: { verdict: Verdict }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="mt-4">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="text-xs text-slate-500 underline hover:text-slate-700"
      >
        {open ? "Hide" : "Show"} raw response
      </button>
      {open && (
        <pre className="mt-2 max-h-96 overflow-auto rounded bg-slate-900 p-3 text-xs text-slate-100">
          {JSON.stringify(verdict, null, 2)}
        </pre>
      )}
    </div>
  );
}
