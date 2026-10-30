// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/chmbrb/PetCommunityProject/internal/config"
	"github.com/chmbrb/PetCommunityProject/internal/ratelimit"
)

// limiters holds one limiter per rate-limit policy.
type limiters struct {
	api, signIn, signUp *ratelimit.Limiter
}

func newLimiters(rl config.RateLimits) limiters {
	return limiters{
		api:    ratelimit.New(rl.API),
		signIn: ratelimit.New(rl.SignIn),
		signUp: ratelimit.New(rl.SignUp),
	}
}

// limitCheck names a limiter for logging.
type limitCheck struct {
	name    string
	limiter *ratelimit.Limiter
}

// rateLimit refuses API requests from clients over their limit with 429 Too Many
// Requests. It runs before authentication, so floods never reach the database.
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if !strings.HasPrefix(path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		key := rateLimitKey(clientIP(r, s.cfg.TrustProxy))

		checks := []limitCheck{{"api", s.limits.api}}
		switch {
		case strings.HasPrefix(path, "/api/v1/auth/signup/"):
			checks = append(checks, limitCheck{"signup", s.limits.signUp})
		case strings.HasPrefix(path, "/api/v1/auth/login/"):
			checks = append(checks, limitCheck{"signin", s.limits.signIn})
		}

		for _, c := range checks {
			if ok, retry := c.limiter.Allow(key); !ok {
				secs := ratelimit.RetryAfterSeconds(retry)
				s.logger.InfoContext(r.Context(), "rate limited",
					"policy", c.name, "path", path, "request_id", requestIDFrom(r.Context()))
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				writeError(w, http.StatusTooManyRequests, "rate_limited", retryMessage(secs))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP returns the address of the client. With trustProxy, it's the rightmost
// X-Forwarded-For entry: the address Caddy saw. Entries further left come from the
// client and can't be trusted. Without trustProxy, it's the TCP peer.
func clientIP(r *http.Request, trustProxy bool) netip.Addr {
	if trustProxy {
		if values := r.Header.Values("X-Forwarded-For"); len(values) > 0 {
			entries := strings.Split(values[len(values)-1], ",")
			if addr, err := netip.ParseAddr(strings.TrimSpace(entries[len(entries)-1])); err == nil {
				return addr.Unmap()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

// rateLimitKey groups IPv6 clients by /64 network, since one subscriber usually
// controls a whole /64 and could otherwise rotate addresses to evade limits.
func rateLimitKey(addr netip.Addr) string {
	switch {
	case !addr.IsValid():
		return "unknown"
	case addr.Is4():
		return addr.String()
	default:
		prefix, err := addr.Prefix(64)
		if err != nil {
			return addr.String()
		}
		return prefix.String()
	}
}

func retryMessage(secs int) string {
	if secs == 1 {
		return "too many requests; try again in 1 second"
	}
	return fmt.Sprintf("too many requests; try again in %d seconds", secs)
}
