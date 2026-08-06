package desktop

import (
	"strings"
	"testing"
)

func TestPartitionMultiRunFailed(t *testing.T) {
	t.Parallel()
	failed, ok := PartitionMultiRunFailed([]MultiRunResult{
		{Name: "b", ExitCode: 0, Stdout: "ok"},
		{Name: "a", ExitCode: 1, Stderr: "nope"},
		{Name: "c", Error: "dial"},
		{Name: "a", ExitCode: 1}, // dup
	})
	if len(failed) != 2 || failed[0] != "a" || failed[1] != "c" {
		t.Fatalf("failed=%v", failed)
	}
	if len(ok) != 1 || ok[0] != "b" {
		t.Fatalf("ok=%v", ok)
	}
}

func TestFormatMultiRunExport(t *testing.T) {
	t.Parallel()
	text := FormatMultiRunExport("uname -a", []MultiRunResult{
		{Name: "z", ExitCode: 0, Stdout: "Linux\n"},
		{Name: "a", ExitCode: 2, Stderr: "boom\n", Stdout: "partial"},
	})
	if !strings.Contains(text, "$ uname -a") {
		t.Fatal(text)
	}
	if !strings.Contains(text, "=== a (FAILED) ===") || !strings.Contains(text, "--- stderr ---") {
		t.Fatal(text)
	}
	if !strings.Contains(text, "=== z (ok) ===") || !strings.Contains(text, "--- stdout ---") {
		t.Fatal(text)
	}
	// Stable name order: a before z
	if strings.Index(text, "=== a") > strings.Index(text, "=== z") {
		t.Fatal("order", text)
	}
}

func TestFormatMultiRunStderrBlock(t *testing.T) {
	t.Parallel()
	s := FormatMultiRunStderrBlock(MultiRunResult{Error: "x", Stderr: "e"})
	if !strings.Contains(s, "error: x") || !strings.Contains(s, "e") {
		t.Fatal(s)
	}
}

func TestMultiRunFailed(t *testing.T) {
	t.Parallel()
	if MultiRunFailed(MultiRunResult{ExitCode: 0}) {
		t.Fatal("ok")
	}
	if !MultiRunFailed(MultiRunResult{ExitCode: 1}) {
		t.Fatal("exit")
	}
}
