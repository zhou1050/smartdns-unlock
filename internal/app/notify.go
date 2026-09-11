package app

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func NotifyTelegram(ctx context.Context, cfg Config, msg string) error {
	if cfg.TGBotToken == "" || cfg.TGChatID == "" {
		return nil
	}
	v := url.Values{}
	v.Set("chat_id", cfg.TGChatID)
	v.Set("text", msg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+cfg.TGBotToken+"/sendMessage", strings.NewReader(v.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cl := http.Client{Timeout: 15 * time.Second}
	r, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	return nil
}
