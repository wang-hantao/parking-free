// MapView — Google Map centered on the user's location with a single
// pin. Drag-to-pan is enabled so users can check "what about over
// there?" without moving physically. The current coords prop is the
// source of truth; clicking elsewhere on the map fires `onPick`
// which the parent can use to refetch the verdict for that point.
//
// Renders nothing if no API key is configured — keeps the rest of the
// app demo-able without paying Google.

import { useEffect } from "react";
import { APIProvider, Map, AdvancedMarker, useMap } from "@vis.gl/react-google-maps";
import { env } from "../lib/env";

interface MapViewProps {
  coords: { lat: number; lng: number };
  onPick?: (coords: { lat: number; lng: number }) => void;
}

// Centre Stockholm fallback if there's a transient issue with coords.
const STOCKHOLM = { lat: 59.3293, lng: 18.0686 };

export function MapView({ coords, onPick }: MapViewProps) {
  if (!env.googleMapsApiKey) {
    return (
      <div className="flex h-full items-center justify-center rounded-lg border border-dashed border-slate-300 bg-slate-100 p-8 text-center text-sm text-slate-600">
        <div>
          <p className="font-medium">Map unavailable</p>
          <p className="mt-1">
            Set <code className="rounded bg-slate-200 px-1">VITE_GOOGLE_MAPS_API_KEY</code> in{" "}
            <code className="rounded bg-slate-200 px-1">.env.local</code> to render the map.
          </p>
        </div>
      </div>
    );
  }

  const centre = coords ?? STOCKHOLM;

  return (
    <APIProvider apiKey={env.googleMapsApiKey}>
      <Map
        mapId="parking-free-map"
        defaultCenter={centre}
        defaultZoom={17}
        gestureHandling="greedy"
        disableDefaultUI={false}
        clickableIcons={false}
        onClick={(e) => {
          if (!onPick) return;
          const ll = e.detail.latLng;
          if (ll) onPick({ lat: ll.lat, lng: ll.lng });
        }}
        className="h-full w-full"
      >
        <AdvancedMarker position={centre} title="Selected point" />
        <Recentrer coords={coords} />
      </Map>
    </APIProvider>
  );
}

// Recentrer keeps the map view in sync when `coords` changes from the
// parent (e.g. the user requests a fresh geolocation). Without it the
// Map's `defaultCenter` only applies on first render.
function Recentrer({ coords }: { coords: { lat: number; lng: number } }) {
  const map = useMap();
  useEffect(() => {
    if (!map) return;
    map.panTo(coords);
  }, [map, coords.lat, coords.lng]);
  return null;
}
