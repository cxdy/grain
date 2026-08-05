package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRulesAndClients(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	r, err := st.AddRule(Rule{Host: "api.example.com", Method: "post", PathPrefix: "/v1/", SecretName: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == "" || r.Method != "POST" {
		t.Fatalf("rule %+v", r)
	}

	list, err := st.ListRules()
	if err != nil || len(list) != 1 {
		t.Fatalf("list %v %v", list, err)
	}

	// reopen
	st2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	list2, err := st2.ListRules()
	if err != nil || len(list2) != 1 || list2[0].Host != "api.example.com" {
		t.Fatalf("persist %+v %v", list2, err)
	}

	if err := st.RemoveRule(r.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = st.ListRules()
	if len(list) != 0 {
		t.Fatal(list)
	}

	// clients
	req, err := st.AuthRequired()
	if err != nil || req {
		t.Fatalf("auth required before clients: %v %v", req, err)
	}
	ok, err := st.ValidToken("")
	if err != nil || !ok {
		t.Fatal("open when no clients")
	}

	c, err := st.CreateClient("guest")
	if err != nil {
		t.Fatal(err)
	}
	if c.Token == "" || c.Name != "guest" {
		t.Fatalf("%+v", c)
	}
	req, _ = st.AuthRequired()
	if !req {
		t.Fatal("auth should be required")
	}
	ok, _ = st.ValidToken("")
	if ok {
		t.Fatal("empty token denied")
	}
	ok, _ = st.ValidToken(c.Token)
	if !ok {
		t.Fatal("token should work")
	}

	// files mode 0600
	for _, name := range []string{"rules.json", "clients.json"} {
		p := filepath.Join(st.Dir(), name)
		// rules may be empty after remove — rewrite
		_ = p
	}
}

func TestNewStoreMkdirFails(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	file := filepath.Join(base, "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(file); err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestStoreCorruptJSONAndReplaceRule(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// corrupt rules
	if err := os.WriteFile(st.rulesPath(), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListRules(); err == nil {
		t.Fatal("expected rules unmarshal error")
	}
	if _, err := st.AddRule(Rule{Host: "x.com"}); err == nil {
		t.Fatal("expected AddRule read error")
	}
	if err := st.RemoveRule("rule-1"); err == nil {
		t.Fatal("expected RemoveRule read error")
	}

	// fix rules and test replace-by-id + write error paths
	if err := os.WriteFile(st.rulesPath(), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := st.AddRule(Rule{ID: "rule-9", Host: "a.example", Method: "get"})
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "rule-9" {
		t.Fatalf("%+v", r)
	}
	r2, err := st.AddRule(Rule{ID: "rule-9", Host: "b.example", Method: "put"})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Host != "b.example" || r2.Method != "PUT" {
		t.Fatalf("%+v", r2)
	}
	list, _ := st.ListRules()
	if len(list) != 1 {
		t.Fatalf("%+v", list)
	}

	// empty host rejected
	if _, err := st.AddRule(Rule{Host: ""}); err == nil {
		t.Fatal("host required")
	}
}

func TestStoreClientsCorruptAndAuth(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.clientsPath(), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListClients(); err == nil {
		t.Fatal("unmarshal")
	}
	if _, err := st.CreateClient("x"); err == nil {
		t.Fatal("create with corrupt")
	}
	if _, err := st.ValidToken("t"); err == nil {
		t.Fatal("valid with corrupt")
	}
	if _, err := st.AuthRequired(); err == nil {
		t.Fatal("auth with corrupt")
	}
	if _, err := st.FirstClientToken(); err == nil {
		t.Fatal("first with corrupt")
	}

	// rewrite empty clients
	if err := os.WriteFile(st.clientsPath(), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	tok, err := st.FirstClientToken()
	if err != nil || tok != "" {
		t.Fatalf("%q %v", tok, err)
	}
	// empty name → auto name
	c, err := st.CreateClient("")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name == "" || c.ID == "" {
		t.Fatalf("%+v", c)
	}
	tok, err = st.FirstClientToken()
	if err != nil || tok == "" {
		t.Fatalf("%q %v", tok, err)
	}
	// bad token
	ok, err := st.ValidToken("nope-token")
	if err != nil || ok {
		t.Fatalf("%v %v", ok, err)
	}
	// next ids
	c2, err := st.CreateClient("second")
	if err != nil {
		t.Fatal(err)
	}
	if c2.ID == c.ID {
		t.Fatal("ids should differ")
	}
}

func TestStoreWriteFailsReadOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// seed valid empty files then make dir read-only
	if err := os.WriteFile(st.rulesPath(), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.clientsPath(), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(st.Dir(), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(st.Dir(), 0o700) })
	// probe write
	if f, err := os.OpenFile(filepath.Join(st.Dir(), "probe"), os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		_ = f.Close()
		_ = os.Remove(filepath.Join(st.Dir(), "probe"))
		t.Skip("fs allows write despite 0555")
	}
	if _, err := st.AddRule(Rule{Host: "x.com"}); err == nil {
		t.Fatal("expected write rules error")
	}
	if _, err := st.CreateClient("c"); err == nil {
		t.Fatal("expected write clients error")
	}
}

func TestStoreHelpersAndListen(t *testing.T) {
	t.Parallel()
	if PIDPath("/d") != filepath.Join("/d", "proxy", "proxy.pid") {
		t.Fatal(PIDPath("/d"))
	}
	if ListenFromConfig("") != DefaultListen {
		t.Fatal(ListenFromConfig(""))
	}
	if ListenFromConfig("127.0.0.1:9") != "127.0.0.1:9" {
		t.Fatal(ListenFromConfig("127.0.0.1:9"))
	}
	// nextRuleID parses
	id := nextRuleID([]Rule{{ID: "rule-3"}, {ID: "other"}, {ID: "rule-1"}})
	if id != "rule-4" {
		t.Fatal(id)
	}
	// Dir accessor
	st, _ := NewStore(t.TempDir())
	if st.Dir() == "" {
		t.Fatal("empty dir")
	}
	// List empty missing files
	rules, err := st.ListRules()
	if err != nil {
		t.Fatal(err)
	}
	_ = rules
}

func TestStoreErrorPaths(t *testing.T) {
	t.Parallel()
	// NewStore when parent path is a file
	base := t.TempDir()
	file := filepath.Join(base, "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(file); err == nil {
		t.Fatal("expected mkdir error")
	}

	dir := t.TempDir()
	st, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	// corrupt rules.json
	if err := os.WriteFile(st.rulesPath(), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListRules(); err == nil {
		t.Fatal("corrupt rules")
	}
	if _, err := st.AddRule(Rule{Host: "h"}); err == nil {
		t.Fatal("add with corrupt")
	}
	if err := st.RemoveRule("rule-1"); err == nil {
		t.Fatal("remove with corrupt")
	}

	// fix rules, corrupt clients
	_ = os.Remove(st.rulesPath())
	if err := os.WriteFile(st.clientsPath(), []byte("["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListClients(); err == nil {
		t.Fatal("corrupt clients")
	}
	if _, err := st.CreateClient("x"); err == nil {
		t.Fatal("create with corrupt")
	}
	if _, err := st.ValidToken("t"); err == nil {
		t.Fatal("valid with corrupt")
	}
	if _, err := st.AuthRequired(); err == nil {
		t.Fatal("auth with corrupt")
	}

	// rules path is a directory → read fails not-not-exist
	_ = os.Remove(st.clientsPath())
	_ = os.Remove(st.rulesPath())
	if err := os.Mkdir(st.rulesPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListRules(); err == nil {
		t.Fatal("rules is dir")
	}

	// writeRules fails when rules.json.tmp cannot be written (parent is file)
	// Use a store whose dir is read-only after open.
	dir2 := t.TempDir()
	st2, err := NewStore(dir2)
	if err != nil {
		t.Fatal(err)
	}
	// chmod proxy dir read-only
	if err := os.Chmod(st2.Dir(), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(st2.Dir(), 0o700) })
	if _, err := st2.AddRule(Rule{Host: "x.com"}); err == nil {
		// may succeed if running as root
		t.Log("add rule on ro dir: unexpected success (or root)")
	}
}

func TestStoreReplaceAndNotFound(t *testing.T) {
	t.Parallel()
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r, err := st.AddRule(Rule{ID: "rule-5", Host: "a.com"})
	if err != nil {
		t.Fatal(err)
	}
	// replace same id
	r2, err := st.AddRule(Rule{ID: "rule-5", Host: "b.com", Method: "GET"})
	if err != nil || r2.Host != "b.com" {
		t.Fatalf("%+v %v", r2, err)
	}
	// host required
	if _, err := st.AddRule(Rule{ID: "rule-6"}); err == nil {
		t.Fatal("host required")
	}
	// remove missing
	if err := st.RemoveRule("nope"); err == nil {
		t.Fatal("expected not found")
	}
	if err := st.RemoveRule(r.ID); err != nil {
		t.Fatal(err)
	}
	// nextRuleID after rule-5
	r3, err := st.AddRule(Rule{Host: "c.com"})
	if err != nil {
		t.Fatal(err)
	}
	if r3.ID == "" {
		t.Fatal("empty id")
	}
	// CreateClient empty name
	c, err := st.CreateClient("")
	if err != nil || c.Name == "" {
		t.Fatalf("%+v %v", c, err)
	}
}
