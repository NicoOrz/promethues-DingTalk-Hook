// 包 dingtalk 封装钉钉群机器人 Webhook 调用与加签逻辑。
package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	httpClient *http.Client
}

func NewClient(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Client{
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

type Message struct {
	MsgType  string
	Title    string
	Markdown string
	Text     string
	At       *At
}

type At struct {
	AtMobiles []string
	AtUserIds []string
	IsAtAll   bool
}

func (c *Client) Send(ctx context.Context, webhook, secret string, msg Message) error {
	webhookURL, err := url.Parse(webhook)
	if err != nil {
		return fmt.Errorf("parse webhook url: %w", err)
	}
	if secret != "" {
		ts := time.Now().UnixMilli()
		sign := Sign(ts, secret)
		q := webhookURL.Query()
		q.Set("timestamp", fmt.Sprintf("%d", ts))
		q.Set("sign", sign)
		webhookURL.RawQuery = q.Encode()
	}

	payload, err := buildPayload(msg)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL.String(), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("post dingtalk: %w", err)
	}
	defer resp.Body.Close()

	var apiResp apiResponse
	_ = json.NewDecoder(resp.Body).Decode(&apiResp)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("dingtalk http %d: %s", resp.StatusCode, apiResp.ErrMsg)
	}
	if apiResp.ErrCode != 0 {
		return fmt.Errorf("dingtalk errcode=%d errmsg=%s", apiResp.ErrCode, apiResp.ErrMsg)
	}
	return nil
}

type apiResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

func buildPayload(msg Message) ([]byte, error) {
	msg = applyAtMentions(msg)

	switch msg.MsgType {
	case "markdown":
		if msg.Markdown == "" {
			return nil, errors.New("markdown content is empty")
		}
		title := msg.Title
		if title == "" {
			title = "Alertmanager"
		}
		payload := map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]any{
				"title": title,
				"text":  msg.Markdown,
			},
		}
		addAt(payload, msg.At)
		return json.Marshal(payload)
	case "text":
		if msg.Text == "" {
			return nil, errors.New("text content is empty")
		}
		payload := map[string]any{
			"msgtype": "text",
			"text": map[string]any{
				"content": msg.Text,
			},
		}
		addAt(payload, msg.At)
		return json.Marshal(payload)
	default:
		return nil, fmt.Errorf("unsupported msg_type %q", msg.MsgType)
	}
}

func applyAtMentions(msg Message) Message {
	if msg.At == nil {
		return msg
	}

	var content *string
	var sep string
	switch msg.MsgType {
	case "markdown":
		content = &msg.Markdown
		sep = "\n\n"
	case "text":
		content = &msg.Text
		sep = "\n"
	default:
		return msg
	}

	if *content == "" {
		return msg
	}
	tokens := mentionTokens(*content, msg.At)
	if len(tokens) == 0 {
		return msg
	}
	*content = *content + sep + strings.Join(tokens, " ")
	return msg
}

func mentionTokens(content string, at *At) []string {
	if at == nil {
		return nil
	}

	if at.IsAtAll {
		if strings.Contains(content, "@all") {
			return nil
		}
		return []string{"@all"}
	}

	out := make([]string, 0, 1+len(at.AtUserIds)+len(at.AtMobiles))
	seen := make(map[string]struct{}, 1+len(at.AtUserIds)+len(at.AtMobiles))

	add := func(v string) {
		v = strings.TrimSpace(v)
		v = strings.TrimPrefix(v, "@")
		if v == "" {
			return
		}

		token := "@" + v
		if strings.Contains(content, token) {
			return
		}
		if _, ok := seen[token]; ok {
			return
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}

	for _, v := range at.AtUserIds {
		add(v)
	}
	for _, v := range at.AtMobiles {
		add(v)
	}

	return out
}

func addAt(payload map[string]any, at *At) {
	if at == nil {
		return
	}
	if !at.IsAtAll && len(at.AtMobiles) == 0 && len(at.AtUserIds) == 0 {
		return
	}
	atPayload := map[string]any{
		"isAtAll": at.IsAtAll,
	}
	if !at.IsAtAll {
		if len(at.AtMobiles) > 0 {
			atPayload["atMobiles"] = at.AtMobiles
		}
		if len(at.AtUserIds) > 0 {
			atPayload["atUserIds"] = at.AtUserIds
		}
	}
	payload["at"] = atPayload
}
