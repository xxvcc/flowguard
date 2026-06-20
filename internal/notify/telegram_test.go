package notify

import (
	"errors"
	"testing"
)

type fakeSender struct {
	calls int
	err   error
}

func (f *fakeSender) Send(text string) error {
	f.calls++
	return f.err
}

func TestNotifierSendsToConfiguredSenders(t *testing.T) {
	sender := &fakeSender{}
	n := Notifier{Senders: []Sender{sender}}
	if err := n.Send("hello"); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 1 {
		t.Fatalf("calls=%d, want 1", sender.calls)
	}
}

func TestNotifierReturnsSenderError(t *testing.T) {
	want := errors.New("boom")
	n := Notifier{Senders: []Sender{&fakeSender{err: want}}}
	if err := n.Send("hello"); !errors.Is(err, want) {
		t.Fatalf("err=%v, want %v", err, want)
	}
}
