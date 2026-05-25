// LocationButton — single trigger that asks the browser for the
// user's location. Visually communicates loading / error states.

interface LocationButtonProps {
  loading: boolean;
  hasCoords: boolean;
  onClick: () => void;
}

export function LocationButton({ loading, hasCoords, onClick }: LocationButtonProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={loading}
      className="inline-flex items-center gap-2 rounded-md bg-slate-900 px-3 py-2 text-sm font-medium text-white shadow-sm transition-colors hover:bg-slate-800 disabled:opacity-60"
    >
      <Pin />
      {loading
        ? "Locating…"
        : hasCoords
        ? "Update my location"
        : "Use my location"}
    </button>
  );
}

function Pin() {
  return (
    <svg
      aria-hidden
      viewBox="0 0 24 24"
      fill="currentColor"
      className="h-4 w-4"
    >
      <path d="M12 2C8.13 2 5 5.13 5 9c0 5.25 7 13 7 13s7-7.75 7-13c0-3.87-3.13-7-7-7zm0 9.5A2.5 2.5 0 1 1 12 6a2.5 2.5 0 0 1 0 5.5z" />
    </svg>
  );
}
