package activity_test

import (
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
}
