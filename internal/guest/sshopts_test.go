package guest

import (
	"slices"
	"testing"
)

func TestSSHArgs_quietAndIdentity(t *testing.T) {
	args := SSHArgs("ubuntu", "127.0.0.1", 2222, "/tmp/id_grain")

	if !containsPair(args, "-o", "LogLevel=ERROR") {
		t.Fatalf("missing LogLevel=ERROR: %v", args)
	}
	if !containsPair(args, "-o", "IdentitiesOnly=yes") {
		t.Fatalf("missing IdentitiesOnly=yes: %v", args)
	}
	if !containsPair(args, "-o", "StrictHostKeyChecking=no") {
		t.Fatalf("missing StrictHostKeyChecking=no: %v", args)
	}
	if !containsPair(args, "-o", "UserKnownHostsFile=/dev/null") {
		t.Fatalf("missing UserKnownHostsFile: %v", args)
	}
	if !containsPair(args, "-o", "GlobalKnownHostsFile=/dev/null") {
		t.Fatalf("missing GlobalKnownHostsFile: %v", args)
	}
	if !containsPair(args, "-o", "UpdateHostKeys=no") {
		t.Fatalf("missing UpdateHostKeys=no: %v", args)
	}
	if !containsPair(args, "-i", "/tmp/id_grain") {
		t.Fatalf("missing -i identity: %v", args)
	}
	if !containsPair(args, "-p", "2222") {
		t.Fatalf("missing -p port: %v", args)
	}
	if args[len(args)-1] != "ubuntu@127.0.0.1" {
		t.Fatalf("expected user@host last, got %v", args)
	}
}

func TestSSHArgs_emptyIdentityOmitsI(t *testing.T) {
	args := SSHArgs("root", "10.0.0.1", 22, "")
	if slices.Contains(args, "-i") {
		t.Fatalf("empty identity should omit -i: %v", args)
	}
	if args[len(args)-1] != "root@10.0.0.1" {
		t.Fatalf("expected user@host last, got %v", args)
	}
}

func TestSCPArgs_quietIdentityAndPort(t *testing.T) {
	args := SCPArgs(2201, "/home/u/.grain/ssh/id_grain")

	if !containsPair(args, "-o", "LogLevel=ERROR") {
		t.Fatalf("missing LogLevel=ERROR: %v", args)
	}
	if !containsPair(args, "-o", "IdentitiesOnly=yes") {
		t.Fatalf("missing IdentitiesOnly=yes: %v", args)
	}
	if !containsPair(args, "-i", "/home/u/.grain/ssh/id_grain") {
		t.Fatalf("missing -i identity: %v", args)
	}
	if !containsPair(args, "-P", "2201") {
		t.Fatalf("scp must use -P for port: %v", args)
	}
	// scp opts only — no user@host
	for _, a := range args {
		if a == "user@host" || (len(a) > 0 && a[0] != '-' && a != "/home/u/.grain/ssh/id_grain" && a != "2201" &&
			a != "StrictHostKeyChecking=no" && a != "UserKnownHostsFile=/dev/null" &&
			a != "GlobalKnownHostsFile=/dev/null" && a != "UpdateHostKeys=no" &&
			a != "LogLevel=ERROR" && a != "IdentitiesOnly=yes") {
			// values after -o / -i / -P are fine; ensure no bare user@host style
			if containsAt(a) {
				t.Fatalf("SCPArgs should not include user@host: %v", args)
			}
		}
	}
}

func TestSSHProbeArgs_batchAndTimeout(t *testing.T) {
	args := SSHProbeArgs("ubuntu", "127.0.0.1", 2222, "/tmp/id_grain")

	if !containsPair(args, "-o", "LogLevel=ERROR") {
		t.Fatalf("missing LogLevel=ERROR: %v", args)
	}
	if !containsPair(args, "-o", "IdentitiesOnly=yes") {
		t.Fatalf("missing IdentitiesOnly=yes: %v", args)
	}
	if !containsPair(args, "-o", "BatchMode=yes") {
		t.Fatalf("missing BatchMode=yes: %v", args)
	}
	if !containsPair(args, "-o", "ConnectTimeout=3") {
		t.Fatalf("missing ConnectTimeout=3: %v", args)
	}
	if !containsPair(args, "-i", "/tmp/id_grain") {
		t.Fatalf("missing -i identity: %v", args)
	}
	if !containsPair(args, "-p", "2222") {
		t.Fatalf("missing -p port: %v", args)
	}
	if len(args) < 3 || args[len(args)-1] != "true" || args[len(args)-2] != "--" {
		t.Fatalf("expected -- true at end: %v", args)
	}
	if args[len(args)-3] != "ubuntu@127.0.0.1" {
		t.Fatalf("expected user@host before --: %v", args)
	}
}

func containsPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func containsAt(s string) bool {
	for _, c := range s {
		if c == '@' {
			return true
		}
	}
	return false
}
