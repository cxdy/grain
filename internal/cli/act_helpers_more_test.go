package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStripLeadingDashDashAndShellJoinMore(t *testing.T) {
	t.Parallel()
	if got := stripLeadingDashDash([]string{"--", "-j", "test"}); len(got) != 2 || got[0] != "-j" {
		t.Fatalf("%v", got)
	}
	if got := stripLeadingDashDash([]string{"-l"}); len(got) != 1 {
		t.Fatalf("%v", got)
	}
	if got := shellJoin([]string{"act", "-j", "my job"}); !strings.Contains(got, "act") {
		t.Fatal(got)
	}
	if shellQuote("") != "''" {
		t.Fatal(shellQuote(""))
	}
	if shellQuote("safe-flag=1") != "safe-flag=1" {
		t.Fatal(shellQuote("safe-flag=1"))
	}
	q := shellQuote("has space")
	if !strings.HasPrefix(q, "'") {
		t.Fatal(q)
	}
	_ = shellQuote("it's")
}

func TestSanitizeActNameMore(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":           "act",
		"My Repo":    "my-repo",
		"---":        "act",
		"9project":   "act-9project",
		"UPPER_case": "upper-case",
	}
	for in, want := range cases {
		if got := sanitizeActName(in); got != want {
			t.Fatalf("%q -> %q want %q", in, got, want)
		}
	}
	long := strings.Repeat("a", 80)
	if got := sanitizeActName(long); len(got) > 48 {
		t.Fatalf("len %d %q", len(got), got)
	}
}

func TestCmdActFlagsParseMore(t *testing.T) {
	t.Parallel()
	cfg := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(cfg, []byte("data_dir: /tmp\n"), 0o644)
	cmd := cmdAct(&cfg)
	cmd.SetArgs([]string{"--keep", "--dir", "/tmp", "--name", "act-x", "--cpus", "2", "--", "-l"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected daemon error")
	}
}

func TestRunGrainActDaemonDownMore(t *testing.T) {
	t.Parallel()
	cfg := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(cfg, []byte("data_dir: "+t.TempDir()+"\n"), 0o644)
	apiURLFlag = "http://127.0.0.1:1"
	t.Cleanup(func() { apiURLFlag = "" })
	err := runGrainAct(&cfg, actOpts{Dir: t.TempDir(), Timeout: time.Second})
	if err == nil {
		t.Fatal("expected error")
	}
}
