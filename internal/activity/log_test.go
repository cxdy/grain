package activity_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cxdy/grain/internal/activity"
)

func TestLogRecordListOrder(t *testing.T) {
	t.Parallel()
	l := activity.New(32)
	l.Record(activity.Event{Action: "create", Target: "a"})
	l.Record(activity.Event{Action: "stop", Target: "a"})
	list := l.List(0)
	if len(list) != 2 {
		t.Fatalf("len %d", len(list))
	}
	if list[0].Action != "stop" || list[1].Action != "create" {
		t.Fatalf("%+v", list)
	}
	if list[0].ID == "" || list[0].ID == list[1].ID {
		t.Fatalf("ids %+v", list)
	}
}

func TestLogCap(t *testing.T) {
	t.Parallel()
	l := activity.New(16)
	for i := 0; i < 30; i++ {
		l.Record(activity.Event{Action: "create", Target: "x"})
	}
	if n := len(l.List(0)); n != 16 {
		t.Fatalf("cap want 16 got %d", n)
	}
}

func TestNewMinCapacity(t *testing.T) {
	t.Parallel()
	// capacity < 16 uses DefaultCapacity (200), so 50 events all fit.
	l := activity.New(4)
	for i := 0; i < 50; i++ {
		l.Record(activity.Event{Action: "x"})
	}
	if n := len(l.List(0)); n != 50 {
		t.Fatalf("want 50 retained under default capacity, got %d", n)
	}
}

func TestListSince(t *testing.T) {
	t.Parallel()
	l := activity.New(32)
	e1 := l.Record(activity.Event{Action: "a"})
	_ = l.Record(activity.Event{Action: "b"})
	e3 := l.Record(activity.Event{Action: "c"})
	// newest first: c, b, a
	since := l.ListSince(e1.ID, 0)
	if len(since) != 2 || since[0].ID != e3.ID {
		t.Fatalf("%+v", since)
	}
	// limit applied
	since2 := l.ListSince(e1.ID, 1)
	if len(since2) != 1 || since2[0].ID != e3.ID {
		t.Fatalf("%+v", since2)
	}
	// empty afterID with limit
	allLim := l.ListSince("", 2)
	if len(allLim) != 2 {
		t.Fatalf("%+v", allLim)
	}
	// unknown afterID → all events (no match)
	unk := l.ListSince("act-99999", 0)
	if len(unk) != 3 {
		t.Fatalf("unknown afterID: %+v", unk)
	}
	// List limit
	if n := len(l.List(1)); n != 1 {
		t.Fatalf("list limit %d", n)
	}
}

func TestNilLog(t *testing.T) {
	t.Parallel()
	var l *activity.Log
	ev := l.Record(activity.Event{Action: "x"})
	if ev.Action != "x" {
		t.Fatalf("%+v", ev)
	}
	if l.List(0) != nil {
		t.Fatal("nil List")
	}
}

func TestRecordPreservesFields(t *testing.T) {
	t.Parallel()
	l := activity.New(32)
	ev := l.Record(activity.Event{
		ID:     "act-custom",
		Time:   "2020-01-01T00:00:00Z",
		Action: "create",
		Status: "error",
	})
	if ev.ID != "act-custom" || ev.Time != "2020-01-01T00:00:00Z" || ev.Status != "error" {
		t.Fatalf("%+v", ev)
	}
	// default status
	ev2 := l.Record(activity.Event{Action: "stop"})
	if ev2.Status != "success" || ev2.ID == "" || ev2.Time == "" {
		t.Fatalf("%+v", ev2)
	}
}

func TestOpenPersistReload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := dir + "/activity.json"
	l, err := activity.Open(path, 32)
	if err != nil {
		t.Fatal(err)
	}
	l.Record(activity.Event{Action: "create", Target: "a", Source: "cli"})
	l.Record(activity.Event{Action: "stop", Target: "a", Source: "desktop"})

	l2, err := activity.Open(path, 32)
	if err != nil {
		t.Fatal(err)
	}
	list := l2.List(0)
	if len(list) != 2 || list[0].Action != "stop" || list[1].Target != "a" {
		t.Fatalf("%+v", list)
	}
	// new ids continue past reloaded seq
	e := l2.Record(activity.Event{Action: "start", Target: "b"})
	if e.ID == list[0].ID {
		t.Fatalf("id collision %s", e.ID)
	}
}

func TestOpenEmptyPathAndMissing(t *testing.T) {
	t.Parallel()
	l, err := activity.Open("", 32)
	if err != nil || l == nil {
		t.Fatalf("%v %v", l, err)
	}
	l.Record(activity.Event{Action: "mem"})
	if len(l.List(0)) != 1 {
		t.Fatal("memory only")
	}

	l2, err := activity.Open(".", 16)
	if err != nil {
		t.Fatal(err)
	}
	_ = l2

	// Open of non-existing nested path is fine (starts empty; creates on write)
	path := filepath.Join(t.TempDir(), "nested", "activity.json")
	l3, err := activity.Open(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	l3.Record(activity.Event{Action: "create"})
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestOpenCorruptAndBareArray(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// corrupt → start fresh but keep path
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("not-json{{{"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := activity.Open(bad, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.List(0)) != 0 {
		t.Fatal("corrupt should start empty")
	}
	l.Record(activity.Event{Action: "after-corrupt"})
	if len(l.List(0)) != 1 {
		t.Fatal("should write after corrupt")
	}

	// bare array format (forward-compat)
	arrPath := filepath.Join(dir, "arr.json")
	arr := []activity.Event{
		{ID: "act-7", Action: "create", Status: "success"},
		{ID: "act-3", Action: "stop", Status: "success"},
		{ID: "not-act", Action: "x", Status: "success"},
	}
	b, _ := json.Marshal(arr)
	if err := os.WriteFile(arrPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	l2, err := activity.Open(arrPath, 16)
	if err != nil {
		t.Fatal(err)
	}
	list := l2.List(0)
	if len(list) != 3 || list[0].ID != "act-7" {
		t.Fatalf("%+v", list)
	}
	// seq inferred from max act-N → next id > 7
	e := l2.Record(activity.Event{Action: "next"})
	if e.ID != "act-8" {
		t.Fatalf("seq infer id %s", e.ID)
	}
}

func TestOpenTruncateToCapacity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "big.json")
	// write snapshot with more events than capacity
	type snap struct {
		Seq    uint64           `json:"seq"`
		Events []activity.Event `json:"events"`
	}
	var events []activity.Event
	for i := 1; i <= 20; i++ {
		events = append(events, activity.Event{ID: "act-" + itoaTest(uint64(i)), Action: "a"})
	}
	b, _ := json.Marshal(snap{Seq: 20, Events: events})
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := activity.Open(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(l.List(0)); n != 16 {
		t.Fatalf("truncate want 16 got %d", n)
	}
	e := l.Record(activity.Event{Action: "more"})
	if e.ID != "act-21" {
		t.Fatalf("seq continue %s", e.ID)
	}
}

func TestPathForDataDir(t *testing.T) {
	t.Parallel()
	if activity.PathForDataDir("") != "" {
		t.Fatal("empty")
	}
	got := activity.PathForDataDir("/data/grain")
	if got != filepath.Join("/data/grain", activity.FileName) {
		t.Fatal(got)
	}
}

func TestOpenWhitespacePath(t *testing.T) {
	t.Parallel()
	// whitespace-only path cleans to "." → memory only
	l, err := activity.Open("   ", 32)
	if err != nil {
		t.Fatal(err)
	}
	l.Record(activity.Event{Action: "x"})
	if len(l.List(0)) != 1 {
		t.Fatal("memory")
	}
}

func TestListSinceEmptyLimitAll(t *testing.T) {
	t.Parallel()
	l := activity.New(32)
	l.Record(activity.Event{Action: "a"})
	all := l.ListSince("", 0)
	if len(all) != 1 {
		t.Fatalf("%+v", all)
	}
}

func itoaTest(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
