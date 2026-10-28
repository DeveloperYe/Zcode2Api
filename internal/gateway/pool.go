package gateway

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"zcode2api/internal/captcha"
	"zcode2api/internal/config"
	"zcode2api/internal/proxy"
	"zcode2api/internal/store"
	"zcode2api/internal/upstream"
)

type Server struct {
	cfg        config.Config
	store      *store.Store
	captcha    captcha.Solver
	sticky     *proxy.StickyProxy
	catalog    *Catalog
	proxies    *proxy.ProxyPool
	up         *upstream.Client
	mu         sync.Mutex
	loginFlows map[string]*loginFlow
	warmOnce   sync.Once
	quotaOnce  sync.Once
	// guard — per-IP brute-force brake on the auth endpoints (ratelimit.go)
	guard *attemptGuard
	// credentials: env values stay in memory as plaintext (they came from
	// outside), settings-page values are kept only as PBKDF2 verifiers.
	// credMu guards the two hash fields — they change at runtime.
	envToken   string
	envGateKey string
	credMu     sync.RWMutex
	adminHash  string
	gwKeyHash  string
	secretOK   *secretCache
	// request limits
	bodyLimit int
	chatSem   chan struct{}
	// runtime knobs (Settings page): quota monitor interval + captcha pool
	quotaRefreshMin int
	poolMin         int
	poolMax         int
	poolTTL         time.Duration
}

func NewServer(cfg config.Config) (*Server, error) {
	st, err := store.OpenStore(config.AccountsDBPath)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:   cfg,
		store: st,
		// actual models come from the origin billing (ModelReport/refresh);
		// only the default model bootstraps the list before the first scan
		catalog:   NewCatalog([]string{config.DefaultModelID}),
		guard:     newAttemptGuard(cfg.AuthRateLimit, cfg.AuthLockAfter, cfg.AuthLockFor()),
		envToken:  cfg.AuthToken,
		envGateKey: cfg.GatewayKey,
		secretOK:  newSecretCache(),
		bodyLimit: cfg.MaxBodyMB << 20,
	}
	if s.bodyLimit <= 0 {
		s.bodyLimit = 8 << 20
	}
	if cfg.MaxConcurrent > 0 {
		s.chatSem = make(chan struct{}, cfg.MaxConcurrent)
	}
	// THE proxy source: profiles in the store (the dashboard edits them).
	// PROXY_FILE only seeds them once on a fresh database; chat and billing
	// egress always read the live profile set.
	s.proxies = &proxy.ProxyPool{}
	s.sticky = &proxy.StickyProxy{Pool: s.proxies}
	s.seedProfilesFromFile()
	s.rebuildEgress()
	if n := s.proxies.Len(); n > 0 {
		log.Printf("[proxies] egress pool: %d lines (from proxy profiles)", n)
	} else {
		log.Printf("[proxies] egress pool empty — origin goes direct")
	}
	// mint output goes to the log only — params are single-use and never
	// replayed, so there is no persisted store
	var node captcha.Solver = captcha.NewNodeSolver(cfg)
	log.Printf("[captcha] solver=node (happy-dom solver subprocess)")
	// pre-solved stock: refill only while a servable account exists
	// (CAPTCHA_MODE=off mutes the background mint entirely)
	s.captcha = captcha.NewMintPool(node, func() bool { return !cfg.CaptchaOff && s.store.Active() != nil },
		s.quotaMin(), s.quotaMax(), s.quotaTTL())
	// Credentials stored by the Settings page live in the DB as PBKDF2
	// verifiers. Legacy plaintext rows (admin_password / gateway_key) are
	// migrated on boot: hash them, write the hash, drop the plaintext.
	s.loadStoredSecrets()
	// decide the auth mode only after the stored settings had their say, then
	// say out loud which mode is in force (the live decision itself is
	// s.passwordless(), never a cached flag)
	if s.passwordless() {
		log.Printf("[auth] NO-PASSWORD MODE: bound to %s, AUTH_TOKEN is empty — the panel and /v1/* are open to this machine only (cross-site browser requests are refused). Set AUTH_TOKEN to require a password.", s.cfg.Host)
	} else if s.envGateKey == "" && s.gwKeyHash == "" {
		log.Printf("[auth] password required; no GATEWAY_KEY set, so /v1/* uses the same credential as the dashboard — consider setting a separate GATEWAY_KEY")
	}
	if s.cfg.AuthRateLimit > 0 || s.cfg.AuthLockAfter > 0 {
		log.Printf("[auth] brute-force guard: %d failed attempts/min per IP, %d consecutive failures → %s lock (TRUST_PROXY=%v)",
			s.cfg.AuthRateLimit, s.cfg.AuthLockAfter, s.cfg.AuthLockFor(), s.cfg.TrustProxy)
	}
	if s.chatSem != nil {
		log.Printf("[limits] body ≤ %d MB, %d concurrent chat requests", s.bodyLimit>>20, cap(s.chatSem))
	} else {
		log.Printf("[limits] body ≤ %d MB, concurrency unlimited (MAX_CONCURRENT=0)", s.bodyLimit>>20)
	}
	// runtime knobs from stored settings (Settings page overrides live)
	s.quotaRefreshMin = atoiSetting(s.store, "quota_refresh_min", 0)
	s.poolMin = atoiSetting(s.store, "captcha_pool_min", 3)
	s.poolMax = atoiSetting(s.store, "captcha_pool_max", 10)
	if d, err := time.ParseDuration(s.store.Setting("captcha_pool_ttl", "95s")); err == nil && d > 0 {
		s.poolTTL = d
	} else {
		s.poolTTL = 95 * time.Second
	}

	s.up = &upstream.Client{
		Cfg:               cfg,
		Captcha:           s.captcha,
		Sticky:            s.sticky,  // chat: one sticky line
		Pool:              s.proxies, // billing family: round-robin spread
		ProfileForAccount: s.profileForAccountMid,
	}
	return s, nil
}

func (s *Server) importKeysFile() {
	n := 0
	for _, k := range config.LoadKeyFile(config.KeysFilePath) {
		if _, err := s.store.Add(k, ""); err == nil {
			n++
		}
	}
	if n > 0 {
		log.Printf("[pool] +%d from %s", n, config.KeysFilePath)
	}
}

func (s *Server) Close() {
	if s.captcha != nil {
		s.captcha.Close()
	}
	if s.store != nil {
		s.store.Close()
	}
}

func (s *Server) activeAccount() *store.Account {
	return s.store.Active()
}

func (s *Server) refreshAllBilling() (checked, dropped int) {
	accs, err := s.store.Accounts()
	if err != nil {
		return 0, 0
	}
	n := s.cfg.BillingWorkers
	if n < 1 {
		n = 1
	}
	if n > 32 {
		n = 32
	}
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	var nChk atomic.Int32
	var markMu sync.Mutex
	expiredIDs := []int64{}
	revokedIDs := []int64{}
	for _, a := range accs {
		a := a
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// billing rides the account's own device mid: warming and quota
			// scans must present ONE device per account, otherwise the origin
			// serves the scan a device it never saw
			q, err := s.up.Billing(a.JWT, a.DeviceMid)
			if err != nil {
				log.Printf("[billing] %s: %v", maskJWT(a.JWT), err)
				// 401 = the token is rejected by the origin: mark, keep the row
				if strings.Contains(fmt.Sprint(err), "HTTP 401") {
					markMu.Lock()
					revokedIDs = append(revokedIDs, a.ID)
					markMu.Unlock()
				}
				return
			}
			nChk.Add(1)
			if upstream.IsEmptyGrant(q) {
				markMu.Lock()
				expiredIDs = append(expiredIDs, a.ID)
				markMu.Unlock()
				return
			}
			s.mu.Lock()
			_ = s.store.UpdateQuota(a.ID, q)
			s.catalog.MergeFromQuota(q)
			s.mu.Unlock()
			log.Printf("[billing] %s plan=%s rem=%d", maskJWT(a.JWT), q.PlanName, q.Remaining)
		}()
	}
	wg.Wait()
	dropped = s.store.MarkStatus(expiredIDs, "expired")
	if n := s.store.MarkStatus(revokedIDs, "revoked"); n > 0 {
		log.Printf("[billing] marked %d revoked (401) — hidden, kept in db", n)
	}
	if dropped > 0 {
		log.Printf("[billing] marked %d empty grants expired — hidden, kept in db", dropped)
	}
	return int(nChk.Load()), dropped
}

func (s *Server) pickExcept(model string, skip map[int64]bool) (*store.Account, error) {
	accs, err := s.store.Accounts()
	if err != nil {
		return nil, err
	}
	if len(accs) == 0 {
		return nil, fmt.Errorf("no accounts")
	}
	dead := s.store.DeadSet()
	mkey := upstream.Norm(model)
	// per-model rotation cursor: each model gets its own round-robin
	// start position
	start := s.store.Rotate("m:"+mkey, len(accs))
	order := append(accs[start:], accs[:start]...)
	for _, a := range order {
		if skip[a.ID] {
			continue
		}
		if a.Status != "" {
			continue // expired or revoked (hidden, kept in db)
		}
		if a.CoolingUntil > time.Now().Unix() {
			continue // risk-control cooldown (strikes)
		}
		if a.DisabledModels != "" {
			skipDisabled := false
			for _, dm := range strings.Split(a.DisabledModels, ",") {
				if strings.TrimSpace(dm) == mkey {
					skipDisabled = true
					break
				}
			}
			if skipDisabled {
				continue // model manually excluded for this account
			}
		}
		if dead[fmt.Sprintf("%d:%s", a.ID, mkey)] {
			continue
		}
		ents := entitlementsOf(a)
		if len(ents) > 0 {
			// the plan was scanned: rotate the account only for models it
			// offers, and only while quota remains (a planless/exhausted
			// account is dead weight for this request)
			if frac, ok := upstream.FracFromEntitlements(ents, model); !ok {
				continue // plan scanned, this model is not in it
			} else if frac <= 0 {
				// quota exhausted — dead until the period reset
				s.store.MarkDead(a.ID, mkey, upstream.DeadUntilUnix(ents, model))
				continue
			}
		}
		return &a, nil
	}
	return nil, fmt.Errorf("pool empty for model %s", model)
}

func (s *Server) markQuotaDead(cur *store.Account, model string) {
	if cur == nil {
		return
	}
	mkey := upstream.Norm(model)
	ents := entitlementsOf(*cur)
	frac, ok := upstream.FracFromEntitlements(ents, model)
	if !ok || frac <= 0 {
		until := upstream.DeadUntilUnix(ents, model)
		s.store.MarkDead(cur.ID, mkey, until)
		log.Printf("[pool] exhausted %s model=%s until %d", maskJWT(cur.JWT), model, until)
		return
	}
	log.Printf("[pool] 1005 %s still %.0f%% — skip this request, not dead", maskJWT(cur.JWT), frac*100)
}

func entitlementsOf(a store.Account) []map[string]any {
	if a.QuotaJSON == "" {
		return nil
	}
	var ents []map[string]any
	_ = json.Unmarshal([]byte(a.QuotaJSON), &ents)
	return ents
}

func maskJWT(j string) string {
	if len(j) < 12 {
		return "***"
	}
	return j[:6] + "…" + j[len(j)-4:]
}

// bearerOf pulls the presented credential out of Authorization: Bearer <x>
// or x-api-key: <x>.
func bearerOf(r *http.Request) string {
	got := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(got) > len(p) && strings.EqualFold(got[:len(p)], p) {
		got = got[len(p):]
	}
	if got == "" {
		got = r.Header.Get("x-api-key")
	}
	return strings.TrimSpace(got)
}

// loadStoredSecrets reads the Settings-page credentials and migrates legacy
// plaintext rows to PBKDF2 verifiers.
func (s *Server) loadStoredSecrets() {
	// admin password
	if v := s.store.Setting("admin_password", ""); v != "" {
		if h, err := hashSecret(v); err == nil {
			s.store.SetSetting(secretKeyAdmin, h)
			s.store.SetSetting("admin_password", "") // drop the plaintext
			log.Printf("[auth] migrated stored admin password to a PBKDF2 verifier")
		} else {
			log.Printf("[auth] could not hash stored admin password: %v", err)
		}
	}
	if v := s.store.Setting("gateway_key", ""); v != "" {
		if h, err := hashSecret(v); err == nil {
			s.store.SetSetting(secretKeyGateway, h)
			s.store.SetSetting("gateway_key", "")
			log.Printf("[auth] migrated stored gateway key to a PBKDF2 verifier")
		} else {
			log.Printf("[auth] could not hash stored gateway key: %v", err)
		}
	}
	s.adminHash = s.store.Setting(secretKeyAdmin, "")
	s.gwKeyHash = s.store.Setting(secretKeyGateway, "")
}

const (
	secretKeyAdmin   = "admin_password_pbkdf2"
	secretKeyGateway = "gateway_key_pbkdf2"
)

// passwordless reports whether NO credential exists at all. It is computed
// live (never cached) because the operator can set a password at runtime —
// caching "there is no password" is exactly how the export gate ended up
// open after a password was set.
func (s *Server) passwordless() bool {
	if s.envToken != "" {
		return false
	}
	s.credMu.RLock()
	defer s.credMu.RUnlock()
	return s.adminHash == ""
}

func (s *Server) adminVerifier() string {
	s.credMu.RLock()
	defer s.credMu.RUnlock()
	return s.adminHash
}

func (s *Server) gatewayVerifier() string {
	s.credMu.RLock()
	defer s.credMu.RUnlock()
	return s.gwKeyHash
}

func (s *Server) setAdminVerifier(h string) {
	s.credMu.Lock()
	s.adminHash = h
	s.credMu.Unlock()
}

func (s *Server) setGatewayVerifier(h string) {
	s.credMu.Lock()
	s.gwKeyHash = h
	s.credMu.Unlock()
}

// checkAdmin verifies a dashboard credential: env token first (it is the
// operator's explicit configuration), then the stored verifier, then the
// loopback no-password mode.
func (s *Server) checkAdmin(presented string) bool {
	if s.envToken != "" {
		return subtle.ConstantTimeCompare([]byte(presented), []byte(s.envToken)) == 1
	}
	if v := s.adminVerifier(); v != "" {
		return s.checkSecret(v, presented)
	}
	return s.passwordless()
}

// checkGateway verifies a /v1/* credential: GATEWAY_KEY / stored gateway
// verifier when configured, otherwise the admin credential.
func (s *Server) checkGateway(presented string) bool {
	if s.envGateKey != "" {
		return subtle.ConstantTimeCompare([]byte(presented), []byte(s.envGateKey)) == 1
	}
	if v := s.gatewayVerifier(); v != "" {
		return s.checkSecret(v, presented)
	}
	return s.checkAdmin(presented)
}

// setAdminPassword stores a new admin verifier (empty clears it, loopback only).
func (s *Server) setAdminPassword(pw string) error {
	if pw == "" {
		s.store.SetSetting(secretKeyAdmin, "")
		s.setAdminVerifier("")
		s.secretOK.reset()
		return nil
	}
	h, err := hashSecret(pw)
	if err != nil {
		return err
	}
	s.store.SetSetting(secretKeyAdmin, h)
	s.setAdminVerifier(h)
	s.secretOK.reset()
	return nil
}

// setGatewayKey stores a new gateway verifier.
func (s *Server) setGatewayKey(key string) error {
	if key == "" {
		s.store.SetSetting(secretKeyGateway, "")
		s.setGatewayVerifier("")
		s.secretOK.reset()
		return nil
	}
	h, err := hashSecret(key)
	if err != nil {
		return err
	}
	s.store.SetSetting(secretKeyGateway, h)
	s.setGatewayVerifier(h)
	s.secretOK.reset()
	return nil
}

// hasPassword reports whether any credential is required at all.
func (s *Server) hasPassword() bool {
	return !s.passwordless()
}

// withGatewayAuth guards /v1/* client endpoints.
func (s *Server) withGatewayAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.enforceAuth(w, r, s.checkGateway, "gateway key") {
			return
		}
		next(w, r)
	}
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.enforceAuth(w, r, s.checkAdmin, "auth token") {
			return
		}
		next(w, r)
	}
}

func oauthBaseEnv() string      { return os.Getenv("ZCODE_OAUTH_API_BASE") }
func exchangeOriginEnv() string { return os.Getenv("ZCODE_EXCHANGE_ORIGIN") }

func atoiSetting(st *store.Store, key string, def int) int {
	if n, err := strconv.Atoi(st.Setting(key, "")); err == nil && n > 0 {
		return n
	}
	return def
}

func (s *Server) quotaMin() int           { return s.poolMin }
func (s *Server) quotaMax() int           { return s.poolMax }
func (s *Server) quotaTTL() time.Duration { return s.poolTTL }
