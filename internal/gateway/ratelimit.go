package gateway

// ratelimit.go — brute-force guard for the authentication endpoints.
//
// /api/status is a natural "is this password right?" oracle: 200 or 401, no
// delay, no lockout, no record. Without a guard, a weak or reused AUTH_TOKEN
// can be guessed online in minutes, and a successful guess hands over the
// whole account pool (the export endpoint returns every JWT).
//
// Two independent brakes, both keyed on the client IP:
//
//   - sliding 1-minute budget of FAILED attempts (default 5/min) → 429
//   - consecutive-failure lockout (default 20 failures → 15 min) → 429
//
// Only rejected credentials consume the budget, so legitimate traffic is
// never throttled. A successful authentication clears the failure counter.
// A lock refuses even the correct password until it expires — that is what
// makes the lock meaningful (otherwise a guesser just retries the winning
// value forever).
//
// Knobs (env): AUTH_RATE_LIMIT (per minute, 0 disables), AUTH_LOCK_AFTER
// (consecutive failures, 0 disables), AUTH_LOCK_MINUTES, TRUST_PROXY.
//
// TRUST_PROXY=1 makes the guard use X-Forwarded-For / X-Real-IP as the client
// identity. Leave it off unless a reverse proxy in front of the server
// rewrites those headers — otherwise a client can spoof them and dodge the
// guard by rotating fake IPs.

import (
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ipAttempts struct {
	windowStart time.Time
	windowFails int
	consecFails int
	lockUntil   time.Time
	lastSeen    time.Time
}

type attemptGuard struct {
	mu      sync.Mutex
	ips     map[string]*ipAttempts
	perMin  int
	lockAt  int
	lockFor time.Duration
	now     func() time.Time
}

const (
	attemptWindow = time.Minute
	// hard cap on tracked IPs: a source spraying random addresses must not be
	// able to grow this map without bound
	maxTrackedIPs = 4096
)

func newAttemptGuard(perMin, lockAt int, lockFor time.Duration) *attemptGuard {
	return &attemptGuard{
		ips:     map[string]*ipAttempts{},
		perMin:  perMin,
		lockAt:  lockAt,
		lockFor: lockFor,
		now:     time.Now,
	}
}

func (g *attemptGuard) state(ip string, now time.Time) *ipAttempts {
	st := g.ips[ip]
	if st == nil {
		if len(g.ips) >= maxTrackedIPs {
			g.sweep(now)
		}
		st = &ipAttempts{windowStart: now}
		g.ips[ip] = st
	}
	st.lastSeen = now
	return st
}

// sweep drops entries that are neither locked nor recently seen.
func (g *attemptGuard) sweep(now time.Time) {
	for ip, st := range g.ips {
		if st.lockUntil.After(now) {
			continue
		}
		if now.Sub(st.lastSeen) > time.Hour {
			delete(g.ips, ip)
		}
	}
	// still too big: drop unlocked entries from the top (map order is random,
	// which is fine — this only runs under memory pressure from an attacker)
	if len(g.ips) >= maxTrackedIPs {
		dropped := 0
		for ip, st := range g.ips {
			if st.lockUntil.After(now) {
				continue
			}
			delete(g.ips, ip)
			dropped++
			if dropped >= maxTrackedIPs/2 {
				break
			}
		}
	}
}

// blocked reports whether this IP must be refused before its credential is
// even examined, and how long the caller should wait.
func (g *attemptGuard) blocked(ip string) (bool, time.Duration) {
	if g == nil {
		return false, 0
	}
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.state(ip, now)
	if st.lockUntil.After(now) {
		return true, st.lockUntil.Sub(now)
	}
	if g.perMin > 0 {
		if now.Sub(st.windowStart) >= attemptWindow {
			st.windowStart = now
			st.windowFails = 0
		}
		if st.windowFails >= g.perMin {
			wait := attemptWindow - now.Sub(st.windowStart)
			if wait < time.Second {
				wait = time.Second
			}
			return true, wait
		}
	}
	return false, 0
}

// fail records a rejected credential. Returns (locked, consecutive failures).
func (g *attemptGuard) fail(ip string) (bool, int) {
	if g == nil {
		return false, 0
	}
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.state(ip, now)
	if now.Sub(st.windowStart) >= attemptWindow {
		st.windowStart = now
		st.windowFails = 0
	}
	st.windowFails++
	st.consecFails++
	if g.lockAt > 0 && st.consecFails >= g.lockAt && g.lockFor > 0 {
		st.lockUntil = now.Add(g.lockFor)
		st.consecFails = 0
		return true, g.lockAt
	}
	return false, st.consecFails
}

// success clears the failure counters for this IP.
func (g *attemptGuard) success(ip string) {
	if g == nil {
		return
	}
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	if st := g.ips[ip]; st != nil {
		st.windowStart = now
		st.windowFails = 0
		st.consecFails = 0
	}
}

// clientIP resolves the caller identity the guard keys on.
func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.IndexByte(xff, ','); i > 0 {
				xff = xff[:i]
			}
			if ip := strings.TrimSpace(xff); ip != "" {
				return ip
			}
		}
		if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" {
			return xr
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// denyAuth writes the 401/429 response for a refused request.
func denyAuth(w http.ResponseWriter, kind string) {
	msg := `{"error":{"message":"invalid ` + kind + `","type":"authentication_error"}}`
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(msg))
}

func throttleAuth(w http.ResponseWriter, wait time.Duration) {
	secs := int(wait.Seconds())
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	w.WriteHeader(http.StatusTooManyRequests)
	w.Write([]byte(`{"error":{"message":"too many failed authentication attempts; retry later","type":"rate_limit_error"}}`))
}

// enforceAuth is the single gate for both /api/* and /v1/*: it applies the
// brute-force guard, then the credential check, and writes the rejection
// itself. Returns true when the request may proceed.
func (s *Server) enforceAuth(w http.ResponseWriter, r *http.Request, check func(string) bool, kind string) bool {
	ip := s.clientIP(r)

	if limited, wait := s.guard.blocked(ip); limited {
		log.Printf("[auth] throttled %s %s ip=%s wait=%s", r.Method, r.URL.Path, ip, wait.Round(time.Second))
		throttleAuth(w, wait)
		return false
	}

	if check(bearerOf(r)) {
		s.guard.success(ip)
		return true
	}

	// only a presented credential counts as a guess; a bare request (stale
	// tab, health probe) must not be able to lock the operator out
	if bearerOf(r) != "" {
		if locked, n := s.guard.fail(ip); locked {
			log.Printf("[auth] LOCKED ip=%s for %s after %d consecutive failures", ip, s.cfg.AuthLockFor(), n)
		}
	}
	log.Printf("[auth] denied %s %s ip=%s ua=%q", r.Method, r.URL.Path, ip, r.UserAgent())
	denyAuth(w, kind)
	return false
}
