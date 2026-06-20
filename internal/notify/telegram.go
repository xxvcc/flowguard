package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.BotToken)
	client := http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(apiURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram returned HTTP %d", resp.StatusCode)
	}
	return nil
}
