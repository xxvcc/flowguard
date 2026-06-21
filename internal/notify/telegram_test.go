package notify

import (
	"errors"
	"strings"
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

func TestTelegramErrorDetailParsesDescription(t *testing.T) {
	detail := telegramErrorDetail(strings.NewReader(`{"ok":false,"description":"Bad Request: chat not found"}`))
	if detail != "Bad Request: chat not found" {
		t.Fatalf("detail=%q", detail)
	}
}

func TestTelegramErrorDetailFallsBackToBody(t *testing.T) {
	detail := telegramErrorDetail(strings.NewReader("plain error"))
	if detail != "plain error" {
		t.Fatalf("detail=%q", detail)
	}
}

func TestRedactTelegramToken(t *testing.T) {
	token := "123456:secret-token"
	message := "telegram failed for https://api.telegram.org/bot123456:secret-token/sendMessage"
	redacted := redactTelegramToken(message, token)
	if strings.Contains(redacted, token) {
		t.Fatalf("token was not redacted: %s", redacted)
	}
	if !strings.Contains(redacted, "<redacted>") {
		t.Fatalf("redaction marker missing: %s", redacted)
	}
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
