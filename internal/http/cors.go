package httpapi

import (
	"net/http"
	"strings"
)

// cors returns a middleware that handles cross-origin requests for
// the API. The set of allowed origins is fixed at server start from
// the configuration, so misconfiguring it can't happen per-request.
//
// Behaviour:
//   - If allowed is empty, the middleware is a no-op (no Access-
//     Control-* headers, no preflight handling). Use this when the
//     frontend is served from the same origin as the API and the
//     browser never issues a preflight.
//   - If allowed contains "*", every Origin is echoed back.
//   - Otherwise the request's Origin is echoed only when it matches
//     one of the configured values exactly. The Vary: Origin header
//     is always set so caches don't conflate responses for different
//     origins.
//
// Preflight (OPTIONS) requests are answered immediately with the
// allowed methods/headers and no body.
func cors(allowed []string) func(http.Handler) http.Handler {
	if len(allowed) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	wildcard := false
	allowSet := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		o = strings.TrimSpace(o)
		if o == "*" {
			wildcard = true
			continue
		}
		if o != "" {
			allowSet[strings.TrimRight(o, "/")] = struct{}{}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			allow := ""
			if wildcard {
				allow = origin
				if allow == "" {
					allow = "*"
				}
			} else if origin != "" {
				if _, ok := allowSet[strings.TrimRight(origin, "/")]; ok {
					allow = origin
				}
			}

			if allow != "" {
				w.Header().Set("Access-Control-Allow-Origin", allow)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key")
				w.Header().Set("Access-Control-Max-Age", "600")
			}

			if r.Method == http.MethodOptions {
				if allow == "" {
					// Disallowed origin: reply 403 rather than 204 so
					// the browser surfaces the rejection clearly.
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
