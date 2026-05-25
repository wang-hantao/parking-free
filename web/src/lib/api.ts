// API client for the parking-free Go backend.
//
// The only endpoint scaffolded here is GET /allowed, which the engine
// uses to answer "can I park at (lat, lng) right now?".
//
// Errors come in two shapes:
//   - Network / CORS failures → thrown Error with `kind: "network"`
//   - HTTP 4xx/5xx → thrown ApiError carrying the status + parsed body
//
// React Query catches both via its `error` channel; components don't
// need to distinguish unless they want to.

import { env } from "./env";
import type { Verdict, VerdictQuery } from "../types/verdict";

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly body: unknown,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

/**
 * GET /allowed
 *
 * Returns the verdict. Pass an `AbortSignal` from React Query so
 * obsolete requests (the user moved to a new location) are cancelled
 * rather than racing to update state.
 */
export async function fetchVerdict(
  q: VerdictQuery,
  signal?: AbortSignal,
): Promise<Verdict> {
  const params = new URLSearchParams({
    lat: q.lat.toFixed(6),
    lng: q.lng.toFixed(6),
    plate: q.plate,
  });
  if (q.class) params.set("class", q.class);
  if (q.at) params.set("at", q.at);
  if (q.radius !== undefined) params.set("radius", String(q.radius));
  if (q.duration_minutes !== undefined) {
    params.set("duration_minutes", String(q.duration_minutes));
  }
  if (q.mode) params.set("mode", q.mode);

  const headers: HeadersInit = { Accept: "application/json" };
  if (env.apiKey) {
    headers["X-API-Key"] = env.apiKey;
  }

  let res: Response;
  try {
    res = await fetch(`${env.apiBaseUrl}/allowed?${params.toString()}`, {
      method: "GET",
      headers,
      signal,
    });
  } catch (e) {
    // Network failure, CORS reject, or AbortError. Re-throw aborts so
    // React Query can identify them; wrap the rest.
    if (e instanceof Error && e.name === "AbortError") throw e;
    throw new Error(
      `Network error reaching ${env.apiBaseUrl}: ${(e as Error).message}`,
    );
  }

  const body: unknown = await safeJson(res);
  if (!res.ok) {
    throw new ApiError(
      res.status,
      body,
      `Verdict request failed: ${res.status} ${res.statusText}`,
    );
  }
  return body as Verdict;
}

async function safeJson(res: Response): Promise<unknown> {
  const text = await res.text();
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}
