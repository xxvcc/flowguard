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
	Config config.Config
}

func (n Notifier) Send(text string) error {
	if !n.Config.Telegram.Enabled {
		return nil
	}
	return sendTelegram(n.Config.Telegram, text)
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
