package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Client is a proxy auth identity (token presented via Proxy-Authorization or URL userinfo).
type Client struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

// Store persists allow rules and clients under dataDir/proxy/.
type Store struct {
	mu  sync.RWMutex
	dir string
	// secrets reader is optional; injected at runtime via Server.
}

// NewStore creates or opens a store at dataDir/proxy.
func NewStore(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "proxy")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Dir returns the proxy state directory.
func (s *Store) Dir() string { return s.dir }

func (s *Store) rulesPath() string   { return filepath.Join(s.dir, "rules.json") }
func (s *Store) clientsPath() string { return filepath.Join(s.dir, "clients.json") }

// --- Rules ---

// ListRules returns all allow rules sorted by id.
func (s *Store) ListRules() ([]Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readRulesUnlocked()
}

func (s *Store) readRulesUnlocked() ([]Rule, error) {
	b, err := os.ReadFile(s.rulesPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rules []Rule
	if err := json.Unmarshal(b, &rules); err != nil {
		return nil, err
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	return rules, nil
}

func (s *Store) writeRulesUnlocked(rules []Rule) error {
	b, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.rulesPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.rulesPath())
}

// AddRule appends a rule (generates id if empty) and persists.
func (s *Store) AddRule(r Rule) (Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rules, err := s.readRulesUnlocked()
	if err != nil {
		return Rule{}, err
	}
	if r.ID == "" {
		r.ID = nextRuleID(rules)
	}
	if r.Host == "" {
		return Rule{}, fmt.Errorf("host is required")
	}
	r.Method = strings.ToUpper(r.Method)
	// Replace if same id exists.
	replaced := false
	for i := range rules {
		if rules[i].ID == r.ID {
			rules[i] = r
			replaced = true
			break
		}
	}
	if !replaced {
		rules = append(rules, r)
	}
	if err := s.writeRulesUnlocked(rules); err != nil {
		return Rule{}, err
	}
	return r, nil
}

// RemoveRule deletes a rule by id.
func (s *Store) RemoveRule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rules, err := s.readRulesUnlocked()
	if err != nil {
		return err
	}
	out := rules[:0]
	found := false
	for _, r := range rules {
		if r.ID == id {
			found = true
			continue
		}
		out = append(out, r)
	}
	if !found {
		return fmt.Errorf("rule %q not found", id)
	}
	return s.writeRulesUnlocked(out)
}

func nextRuleID(rules []Rule) string {
	max := 0
	for _, r := range rules {
		var n int
		if _, err := fmt.Sscanf(r.ID, "rule-%d", &n); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("rule-%d", max+1)
}

// --- Clients ---

// ListClients returns all proxy clients (including tokens — local-only store).
func (s *Store) ListClients() ([]Client, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readClientsUnlocked()
}

func (s *Store) readClientsUnlocked() ([]Client, error) {
	b, err := os.ReadFile(s.clientsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var clients []Client
	if err := json.Unmarshal(b, &clients); err != nil {
		return nil, err
	}
	sort.Slice(clients, func(i, j int) bool { return clients[i].ID < clients[j].ID })
	return clients, nil
}

func (s *Store) writeClientsUnlocked(clients []Client) error {
	b, err := json.MarshalIndent(clients, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.clientsPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.clientsPath())
}

// CreateClient creates a client with a random token.
func (s *Store) CreateClient(name string) (Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	clients, err := s.readClientsUnlocked()
	if err != nil {
		return Client{}, err
	}
	if name == "" {
		name = nextClientName(clients)
	}
	tok, err := randomToken(24)
	if err != nil {
		return Client{}, err
	}
	c := Client{
		ID:        nextClientID(clients),
		Name:      name,
		Token:     tok,
		CreatedAt: time.Now().UTC(),
	}
	clients = append(clients, c)
	if err := s.writeClientsUnlocked(clients); err != nil {
		return Client{}, err
	}
	return c, nil
}

// ValidToken reports whether token matches any client.
// When no clients exist, returns true (auth optional / open until first client).
func (s *Store) ValidToken(token string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	clients, err := s.readClientsUnlocked()
	if err != nil {
		return false, err
	}
	if len(clients) == 0 {
		return true, nil
	}
	if token == "" {
		return false, nil
	}
	for _, c := range clients {
		if c.Token == token {
			return true, nil
		}
	}
	return false, nil
}

// AuthRequired reports whether any clients exist (proxy requires a token).
func (s *Store) AuthRequired() (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	clients, err := s.readClientsUnlocked()
	if err != nil {
		return false, err
	}
	return len(clients) > 0, nil
}

// FirstClientToken returns the first client's token if any.
func (s *Store) FirstClientToken() (string, error) {
	clients, err := s.ListClients()
	if err != nil {
		return "", err
	}
	if len(clients) == 0 {
		return "", nil
	}
	return clients[0].Token, nil
}

func nextClientID(clients []Client) string {
	max := 0
	for _, c := range clients {
		var n int
		if _, err := fmt.Sscanf(c.ID, "client-%d", &n); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("client-%d", max+1)
}

func nextClientName(clients []Client) string {
	return fmt.Sprintf("client-%d", len(clients)+1)
}

func randomToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// PIDPath returns the default pid file path for the proxy process.
func PIDPath(dataDir string) string {
	return filepath.Join(dataDir, "proxy", "proxy.pid")
}

// ListenFromConfig returns listen addr or the default 0.0.0.0:3128.
func ListenFromConfig(addr string) string {
	if addr == "" {
		return DefaultListen
	}
	return addr
}

// DefaultListen is reachable by SLIRP guests via 10.0.2.2.
const DefaultListen = "0.0.0.0:3128"

// DefaultGuestProxyHost is the QEMU SLIRP gateway (host from guest).
const DefaultGuestProxyHost = "10.0.2.2"

// DefaultGuestProxyPort is the default proxy port as seen by the guest.
const DefaultGuestProxyPort = 3128
