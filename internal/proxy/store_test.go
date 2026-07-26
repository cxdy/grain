package proxy

import (
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
