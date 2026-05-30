// PaymentLinks — buttons that launch the user's chosen parking app.
//
// The backend's `pricing.operators` array carries the four
// Stockholm-authorised operators (EasyPark, Parkster, Mobill, ePARK)
// with their deeplink templates expanded for the current plate. We
// render each as a tappable button.
//
// If the server's deeplink template carries an `app_url`, we use it
// directly. Otherwise we fall back to the operator's web URL so the
// user at least gets the operator's site as a starting point.
//
// Mobile: the deep-links launch the native app via universal links
// (iOS) / intent URIs (Android) when the app is installed, falling
// back to the App Store / Play Store otherwise. Desktop: most
// operators' web URLs prompt to open the mobile app via QR or open a
// browser-based payment flow.

import type { OperatorOption } from "../types/verdict";

// Brand colours — keeps the buttons recognisable. Approximations
// based on each operator's public branding; tweak if/when we get
// official assets.
const BRAND: Record<string, { bg: string; hover: string; fg: string }> = {
  easypark: { bg: "bg-emerald-600", hover: "hover:bg-emerald-700", fg: "text-white" },
  parkster: { bg: "bg-orange-500", hover: "hover:bg-orange-600", fg: "text-white" },
  mobill: { bg: "bg-sky-600", hover: "hover:bg-sky-700", fg: "text-white" },
  epark: { bg: "bg-slate-800", hover: "hover:bg-slate-900", fg: "text-white" },
};

const DEFAULT_BRAND = { bg: "bg-slate-700", hover: "hover:bg-slate-800", fg: "text-white" };

interface PaymentLinksProps {
  operators: OperatorOption[];
}

export function PaymentLinks({ operators }: PaymentLinksProps) {
  if (operators.length === 0) return null;
  return (
    <div>
      <p className="text-sm font-medium text-slate-700">Pay via a parking app</p>
      <p className="mt-0.5 text-xs text-slate-500">
        Any of these operators can take payment for this spot.
      </p>
      <div className="mt-2 flex flex-wrap gap-2">
        {operators.map((op) => {
          const url = resolveLaunchUrl(op);
          const style = BRAND[op.id] ?? DEFAULT_BRAND;
          return (
            <a
              key={op.id}
              href={url}
              target={url.startsWith("http") ? "_blank" : undefined}
              rel="noopener noreferrer"
              className={`inline-flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium ${style.bg} ${style.hover} ${style.fg} transition-colors`}
            >
              {op.name}
              {op.external_zone_id && (
                <span className="rounded bg-black/15 px-1.5 py-0.5 text-xs">
                  zone {op.external_zone_id}
                </span>
              )}
            </a>
          );
        })}
      </div>
    </div>
  );
}

// resolveLaunchUrl picks the URL to launch:
//   1. Server-resolved `deeplink` (zone-specific or city-wide).
//   2. A hardcoded web fallback for the four Stockholm operators.
function resolveLaunchUrl(op: OperatorOption): string {
  if (op.deeplink) return op.deeplink;
  return webFallback(op);
}

function webFallback(op: OperatorOption): string {
  switch (op.id) {
    case "easypark":
      return "https://web.easypark.net/";
    case "parkster":
      return "https://parkster.com/";
    case "mobill":
      return "https://mobill.se/";
    case "epark":
      return "https://www.epark.se/";
    default:
      return "#";
  }
}
