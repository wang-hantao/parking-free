// useGeolocation — thin wrapper around navigator.geolocation that
// fits React's state model.
//
// Deliberately does NOT auto-request on mount: the browser permission
// prompt can be jarring on first load, and users without secure
// contexts (http://, some embedded webviews) would silently fail.
// Components call `request()` from a click handler instead.

import { useCallback, useState } from "react";

export interface GeoCoords {
  lat: number;
  lng: number;
  accuracy: number; // metres
  timestamp: number;
}

export interface GeoError {
  code: "denied" | "unavailable" | "timeout" | "unsupported";
  message: string;
}

export interface GeoState {
  coords: GeoCoords | null;
  error: GeoError | null;
  loading: boolean;
}

interface UseGeolocation extends GeoState {
  request: () => void;
  clear: () => void;
}

export function useGeolocation(): UseGeolocation {
  const [state, setState] = useState<GeoState>({
    coords: null,
    error: null,
    loading: false,
  });

  const request = useCallback(() => {
    if (!("geolocation" in navigator)) {
      setState({
        coords: null,
        loading: false,
        error: {
          code: "unsupported",
          message: "Your browser does not expose geolocation.",
        },
      });
      return;
    }

    setState((s) => ({ ...s, loading: true, error: null }));

    navigator.geolocation.getCurrentPosition(
      (pos) =>
        setState({
          loading: false,
          error: null,
          coords: {
            lat: pos.coords.latitude,
            lng: pos.coords.longitude,
            accuracy: pos.coords.accuracy,
            timestamp: pos.timestamp,
          },
        }),
      (err) =>
        setState({
          loading: false,
          coords: null,
          error: mapPositionError(err),
        }),
      {
        enableHighAccuracy: true,
        maximumAge: 15_000, // accept a 15s cached fix
        timeout: 10_000,
      },
    );
  }, []);

  const clear = useCallback(() => {
    setState({ coords: null, error: null, loading: false });
  }, []);

  return { ...state, request, clear };
}

function mapPositionError(e: GeolocationPositionError): GeoError {
  switch (e.code) {
    case e.PERMISSION_DENIED:
      return {
        code: "denied",
        message:
          "Location permission was denied. You can grant it in your browser's site settings and try again.",
      };
    case e.POSITION_UNAVAILABLE:
      return {
        code: "unavailable",
        message: "Your device can't determine its location right now.",
      };
    case e.TIMEOUT:
      return {
        code: "timeout",
        message: "Getting your location took too long. Try again.",
      };
    default:
      return {
        code: "unavailable",
        message: e.message || "Unknown geolocation error.",
      };
  }
}
