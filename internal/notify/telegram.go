package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"flowguard/internal/config"
)

type Notifier struct {
	Config  config.Config
	Senders []Sender
}

type Sender interface {
	Send(text string) error
}

type TelegramSender struct {
	Config config.Telegram
}

func New(cfg config.Config) Notifier {
	var senders []Sender
	if cfg.Telegram.Enabled {
		senders = append(senders, TelegramSender{Config: cfg.Telegram})
	}
	return Notifier{Config: cfg, Senders: senders}
}

func (n Notifier) Send(text string) error {
	senders := n.Senders
	if senders == nil {
		senders = New(n.Config).Senders
	}
	for _, sender := range senders {
		if err := sender.Send(text); err != nil {
			return err
		}
	}
	return nil
}

func (s TelegramSender) Send(text string) error {
	return sendTelegram(s.Config, text)
}

func sendTelegram(cfg config.Telegram, text string) error {
	payload := map[string]string{
		"chat_id": cfg.ChatID,
		"text":    text,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// PathEscape the token so a malformed token (e.g. one containing "/" or "..")
	// cannot alter the request path; a well-formed Telegram token is unaffected.
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", url.PathEscape(cfg.BotToken))
	client := http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(apiURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return redactTelegramSecret(err.Error(), cfg.BotToken)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram returned HTTP %d: %s", resp.StatusCode, redactTelegramSecret(telegramErrorDetail(resp.Body), cfg.BotToken))
	}
	return nil
}

func telegramErrorDetail(body io.Reader) string {
	data, err := io.ReadAll(io.LimitReader(body, 4096))
	if err != nil || len(data) == 0 {
		return "empty response"
	}
	var parsed struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(data, &parsed); err == nil && strings.TrimSpace(parsed.Description) != "" {
		return parsed.Description
	}
	return strings.TrimSpace(string(data))
}

func redactTelegramSecret(message string, token string) error {
	return fmt.Errorf("%s", redactTelegramToken(message, token))
}

func redactTelegramToken(message string, token string) string {
	if token == "" {
		return message
	}
	return strings.ReplaceAll(message, token, "<redacted>")
}
