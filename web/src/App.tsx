// App — top-level component orchestrating the flow:
//
//   1. User clicks "Use my location" → browser prompts → coords land
//      in useGeolocation state.
//   2. coords change → useParkingVerdict kicks off (or refetches) a
//      GET /allowed call.
//   3. Map recentres on coords. VerdictCard renders the response.
//   4. User can drag/click the map to query a different point.
//
// Plate is held in component state for now with a sensible demo
// default. Real signup/auth lives in a future iteration.

import { useState } from "react";
import { LocationButton } from "./components/LocationButton";
import { MapView } from "./components/MapView";
import { VerdictCard } from "./components/VerdictCard";
import { useGeolocation } from "./hooks/useGeolocation";
import { useParkingVerdict } from "./hooks/useParkingVerdict";

export default function App() {
  const geo = useGeolocation();
  const [plate, setPlate] = useState("ABC123");
  const [mode, setMode] = useState<"nearby" | "strict">("strict");
  const [pickedCoords, setPickedCoords] = useState<{ lat: number; lng: number } | null>(null);

  // Picked-on-map coords override geolocation coords so users can
  // explore points other than their current physical location.
  const coords = pickedCoords ?? geo.coords;

  const verdict = useParkingVerdict({
    coords,
    plate,
    mode,
    enabled: plate.trim().length > 0,
  });

  return (
    <div className="mx-auto flex min-h-screen max-w-6xl flex-col gap-4 p-4 lg:p-6">
      <Header />

      <div className="flex flex-wrap items-center gap-3">
        <LocationButton
          loading={geo.loading}
          hasCoords={Boolean(coords)}
          onClick={() => {
            setPickedCoords(null);
            geo.request();
          }}
        />

        <label className="flex items-center gap-2 text-sm">
          <span className="text-slate-600">Plate</span>
          <input
            type="text"
            value={plate}
            onChange={(e) => setPlate(e.target.value.toUpperCase().slice(0, 10))}
            placeholder="ABC123"
            className="w-32 rounded-md border border-slate-300 px-2 py-1.5 font-mono text-sm focus:border-slate-500 focus:outline-none"
          />
        </label>

        <label className="flex items-center gap-2 text-sm">
          <span className="text-slate-600">Mode</span>
          <select
            value={mode}
            onChange={(e) => setMode(e.target.value as "nearby" | "strict")}
            className="rounded-md border border-slate-300 px-2 py-1.5 text-sm focus:border-slate-500 focus:outline-none"
          >
            <option value="strict">Strict (this spot)</option>
            <option value="nearby">Nearby (50m radius)</option>
          </select>
        </label>

        {pickedCoords && (
          <button
            type="button"
            onClick={() => setPickedCoords(null)}
            className="text-sm text-slate-600 underline hover:text-slate-900"
          >
            Use my location again
          </button>
        )}
      </div>

      <GeoErrorBanner error={geo.error} />

      <div className="grid flex-1 grid-cols-1 gap-4 lg:grid-cols-5">
        <div className="lg:col-span-3">
          <div className="h-[420px] overflow-hidden rounded-xl shadow-sm ring-1 ring-slate-200 lg:h-[600px]">
            {coords ? (
              <MapView coords={coords} onPick={setPickedCoords} />
            ) : (
              <div className="flex h-full items-center justify-center bg-slate-100 p-6 text-center text-sm text-slate-500">
                Click <strong>Use my location</strong> above to get started.
              </div>
            )}
          </div>
        </div>

        <div className="space-y-3 lg:col-span-2">
          {verdict.isLoading && coords && <SkeletonCard />}
          {verdict.error && (
            <ErrorCard message={verdict.error.message} />
          )}
          {verdict.data && <VerdictCard verdict={verdict.data} />}
          {coords && !verdict.data && !verdict.isLoading && !verdict.error && (
            <p className="rounded-lg bg-slate-100 p-4 text-sm text-slate-500">
              Waiting for a verdict — make sure your backend is running at the configured
              URL.
            </p>
          )}
        </div>
      </div>

      <Footer />
    </div>
  );
}

function Header() {
  return (
    <header className="flex items-center justify-between">
      <div>
        <h1 className="text-xl font-semibold text-slate-900">Parking Free</h1>
        <p className="text-sm text-slate-500">
          Can I park here right now? Defensible answers backed by Stockholm LTF-Tolken data.
        </p>
      </div>
    </header>
  );
}

function Footer() {
  return (
    <footer className="mt-2 text-xs text-slate-400">
      Data: Stockholm LTF-Tolken. Verdicts include a citation trail; cross-check before
      relying on them for legal disputes.
    </footer>
  );
}

function GeoErrorBanner({ error }: { error: ReturnType<typeof useGeolocation>["error"] }) {
  if (!error) return null;
  return (
    <div className="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-900 ring-1 ring-amber-300">
      <strong className="font-medium">Location:</strong> {error.message}
    </div>
  );
}

function SkeletonCard() {
  return (
    <div className="animate-pulse rounded-xl bg-white p-5 shadow-sm ring-1 ring-slate-200">
      <div className="h-4 w-32 rounded bg-slate-200" />
      <div className="mt-3 h-3 w-48 rounded bg-slate-200" />
      <div className="mt-2 h-3 w-40 rounded bg-slate-200" />
    </div>
  );
}

function ErrorCard({ message }: { message: string }) {
  return (
    <div className="rounded-xl bg-forbidden-50 p-5 text-sm text-forbidden-700 ring-1 ring-forbidden-500">
      <p className="font-medium">Couldn't reach the backend</p>
      <p className="mt-1 break-words">{message}</p>
      <p className="mt-2 text-xs text-forbidden-700/80">
        Check VITE_API_BASE_URL in <code>.env.local</code> and that the server is reachable
        with CORS configured for this origin.
      </p>
    </div>
  );
}
