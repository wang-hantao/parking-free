// useParkingVerdict — React Query wrapper around fetchVerdict.
//
// - Keyed on (lat, lng, plate, mode) so changing any of them refetches
// - 60s staleTime so quick double-clicks don't refetch
// - Auto-disabled when there are no coords yet
// - Retries once on network errors but not on HTTP 4xx (4xx is a
//   client problem; refetching won't help)

import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import type { Verdict, VerdictQuery } from "../types/verdict";
import { ApiError, fetchVerdict } from "../lib/api";

interface UseParkingVerdictInput {
  coords: { lat: number; lng: number } | null;
  plate: string;
  mode?: "nearby" | "strict";
  durationMinutes?: number;
  enabled?: boolean;
}

export function useParkingVerdict({
  coords,
  plate,
  mode,
  durationMinutes,
  enabled = true,
}: UseParkingVerdictInput): UseQueryResult<Verdict, Error> {
  return useQuery<Verdict, Error>({
    queryKey: [
      "verdict",
      coords?.lat?.toFixed(6),
      coords?.lng?.toFixed(6),
      plate,
      mode ?? "nearby",
      durationMinutes ?? null,
    ],
    enabled: enabled && coords !== null && plate.length > 0,
    staleTime: 60_000,
    gcTime: 5 * 60_000,
    retry: (failureCount, error) => {
      // 4xx → don't retry. 5xx and network errors → retry once.
      if (error instanceof ApiError && error.status >= 400 && error.status < 500) {
        return false;
      }
      return failureCount < 1;
    },
    queryFn: async ({ signal }) => {
      if (!coords) {
        // Should not happen given `enabled`, but TypeScript can't tell.
        throw new Error("no coords");
      }
      const q: VerdictQuery = {
        lat: coords.lat,
        lng: coords.lng,
        plate,
      };
      if (mode) q.mode = mode;
      if (durationMinutes !== undefined) q.duration_minutes = durationMinutes;
      return fetchVerdict(q, signal);
    },
  });
}
