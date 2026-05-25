// Env access — kept in one place so misconfigurations fail at boot
// rather than when a user clicks something.
//
// `import.meta.env` is Vite's compile-time-replaced env access.
// Anything prefixed with VITE_ becomes a string literal in the built
// bundle. Do not put secrets here; they will be visible to anyone
// who opens DevTools.

interface Env {
  apiBaseUrl: string;
  googleMapsApiKey: string;
  apiKey: string; // optional; backend can run without auth
}

function readEnv(): Env {
  const apiBaseUrl = import.meta.env.VITE_API_BASE_URL?.replace(/\/+$/, "");
  if (!apiBaseUrl) {
    throw new Error(
      "VITE_API_BASE_URL is not set. Copy web/.env.example to .env.local and configure it.",
    );
  }

  const googleMapsApiKey = import.meta.env.VITE_GOOGLE_MAPS_API_KEY;
  if (!googleMapsApiKey) {
    // Don't throw — let the Map component render a fallback so the
    // rest of the app remains usable without a key (useful in CI and
    // for developers without one yet).
    console.warn(
      "VITE_GOOGLE_MAPS_API_KEY is empty. The map will not render. " +
        "Get one at https://console.cloud.google.com/google/maps-apis/credentials",
    );
  }

  return {
    apiBaseUrl,
    googleMapsApiKey: googleMapsApiKey ?? "",
    apiKey: import.meta.env.VITE_API_KEY ?? "",
  };
}

export const env = readEnv();
