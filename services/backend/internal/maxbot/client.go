package maxbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"zdorovie/backend/internal/config"
)

// Button represents a MAX inline keyboard button (type: callback | link).
type Button struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Payload string `json:"payload,omitempty"`
	URL     string `json:"url,omitempty"`
}

// InlineKeyboardPayload wraps rows of MAX inline buttons (1-2 per row recommended).
type InlineKeyboardPayload struct {
	Buttons [][]Button `json:"buttons"`
}

// Attachment represents a MAX message attachment (inline_keyboard).
type Attachment struct {
	Type    string                `json:"type"`
	Payload InlineKeyboardPayload `json:"payload"`
}

// OutgoingMessage is the payload for POST /messages or POST /answers.
type OutgoingMessage struct {
	Text        string       `json:"text"`
	Format      string       `json:"format,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// AnswerBody wraps message replacement for POST /answers?callback_id=...
type AnswerBody struct {
	Message      *OutgoingMessage `json:"message,omitempty"`
	Notification string           `json:"notification,omitempty"`
}

// SentRecord captures outbound messages in mock mode for local simulator & tests.
type SentRecord struct {
	Timestamp  time.Time       `json:"timestamp"`
	UserID     string          `json:"user_id"`
	CallbackID string          `json:"callback_id,omitempty"`
	Kind       string          `json:"kind"` // message | answer
	Message    OutgoingMessage `json:"message"`
}

// Messenger abstracts MAX Bot API outbound calls.
type Messenger interface {
	SendMessage(ctx context.Context, userID string, msg OutgoingMessage) error
	AnswerCallback(ctx context.Context, callbackID string, body AnswerBody) error
}

// Client implements Messenger with MAX rate limiting (30 RPS global, 2 RPS per dialog).
type Client struct {
	cfg            config.Config
	httpClient     *http.Client
	pollHTTPClient *http.Client
	globalLim      *rate.Limiter
	userMu         sync.Mutex
	userLims       map[string]*rate.Limiter

	mockMu sync.Mutex
	sent   []SentRecord
}

// NewClient initializes the rate-limited MAX Bot API client.
func NewClient(cfg config.Config) *Client {
	return &Client{
		cfg:            cfg,
		httpClient:     &http.Client{Timeout: 10 * time.Second},
		pollHTTPClient: &http.Client{Timeout: 40 * time.Second},
		globalLim:      rate.NewLimiter(rate.Limit(30), 30),
		userLims:       make(map[string]*rate.Limiter),
	}
}

// IsLive reports whether live MAX Bot API calls are enabled with a non-placeholder token.
func (c *Client) IsLive() bool {
	return strings.EqualFold(c.cfg.MaxMode, "live") &&
		c.cfg.MaxBotToken != "" &&
		c.cfg.MaxBotToken != "test_max_bot_token"
}

func (c *Client) baseURL() string {
	return strings.TrimRight(c.cfg.MaxAPIURL, "/")
}

func (c *Client) userLimiter(userID string) *rate.Limiter {
	c.userMu.Lock()
	defer c.userMu.Unlock()
	if l, ok := c.userLims[userID]; ok {
		return l
	}
	l := rate.NewLimiter(rate.Limit(2), 2)
	c.userLims[userID] = l
	return l
}

func (c *Client) waitLimits(ctx context.Context, userID string) error {
	if err := c.globalLim.Wait(ctx); err != nil {
		return err
	}
	if userID != "" {
		if err := c.userLimiter(userID).Wait(ctx); err != nil {
			return err
		}
	}
	return nil
}

// SendMessage sends a message with optional inline keyboard via POST /messages?user_id=...
func (c *Client) SendMessage(ctx context.Context, userID string, msg OutgoingMessage) error {
	if msg.Format == "" {
		msg.Format = "markdown"
	}
	if len([]rune(msg.Text)) > 4096 {
		msg.Text = string([]rune(msg.Text)[:4093]) + "..."
	}

	if !c.IsLive() {
		c.recordMock(SentRecord{
			Timestamp: time.Now().UTC(),
			UserID:    userID,
			Kind:      "message",
			Message:   msg,
		})
		log.Printf("[MAX Mock SendMessage] user_id=%s text_len=%d buttons=%d", userID, len(msg.Text), countButtons(msg))
		return nil
	}

	if err := c.waitLimits(ctx, userID); err != nil {
		return err
	}
	msg = sanitizeForLiveMAX(msg)
	endpoint := fmt.Sprintf("%s/messages?user_id=%s", c.baseURL(), url.QueryEscape(userID))
	return c.postJSON(ctx, endpoint, msg)
}

// AnswerCallback acknowledges a button click and optionally edits the message via POST /answers?callback_id=...
func (c *Client) AnswerCallback(ctx context.Context, callbackID string, body AnswerBody) error {
	if body.Message != nil && body.Message.Format == "" {
		body.Message.Format = "markdown"
	}
	if !c.IsLive() {
		var m OutgoingMessage
		if body.Message != nil {
			m = *body.Message
		}
		c.recordMock(SentRecord{
			Timestamp:  time.Now().UTC(),
			CallbackID: callbackID,
			Kind:       "answer",
			Message:    m,
		})
		log.Printf("[MAX Mock AnswerCallback] callback_id=%s", callbackID)
		return nil
	}

	if err := c.waitLimits(ctx, ""); err != nil {
		return err
	}
	if body.Message != nil {
		sanitized := sanitizeForLiveMAX(*body.Message)
		body.Message = &sanitized
	}
	endpoint := fmt.Sprintf("%s/answers?callback_id=%s", c.baseURL(), url.QueryEscape(callbackID))
	return c.postJSON(ctx, endpoint, body)
}

// sanitizeForLiveMAX moves non-HTTPS link buttons (e.g. local http://localhost dev URLs) into the message body
// because MAX Bot API requires https:// URLs in inline_keyboard link buttons.
func sanitizeForLiveMAX(msg OutgoingMessage) OutgoingMessage {
	if len(msg.Attachments) == 0 {
		return msg
	}
	var fallbackLinks []string
	var cleanAttachments []Attachment
	for _, att := range msg.Attachments {
		if att.Type != "inline_keyboard" {
			cleanAttachments = append(cleanAttachments, att)
			continue
		}
		var cleanRows [][]Button
		for _, row := range att.Payload.Buttons {
			var cleanRow []Button
			for _, btn := range row {
				if btn.Type == "link" && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(btn.URL)), "https://") {
					if strings.TrimSpace(btn.URL) != "" {
						fallbackLinks = append(fallbackLinks, fmt.Sprintf("🔗 %s: `%s`", btn.Text, btn.URL))
					}
					continue
				}
				cleanRow = append(cleanRow, btn)
			}
			if len(cleanRow) > 0 {
				cleanRows = append(cleanRows, cleanRow)
			}
		}
		if len(cleanRows) > 0 {
			att.Payload.Buttons = cleanRows
			cleanAttachments = append(cleanAttachments, att)
		}
	}
	msg.Attachments = cleanAttachments
	if len(fallbackLinks) > 0 {
		msg.Text = strings.TrimRight(msg.Text, "\n") + "\n\n" + strings.Join(fallbackLinks, "\n")
		if len([]rune(msg.Text)) > 4096 {
			msg.Text = string([]rune(msg.Text)[:4093]) + "..."
		}
	}
	return msg
}

// RegisterSubscription registers the webhook URL in MAX Bot API (POST /subscriptions).
func (c *Client) RegisterSubscription(ctx context.Context, webhookURL string) error {
	payload := map[string]interface{}{
		"url":          webhookURL,
		"update_types": []string{"bot_started", "message_created", "message_callback", "user_added", "user_removed"},
		"secret":       c.cfg.MaxWebhookSecret,
	}
	if !c.IsLive() {
		log.Printf("[MAX Mock RegisterSubscription] url=%s", webhookURL)
		return nil
	}
	endpoint := fmt.Sprintf("%s/subscriptions", c.baseURL())
	return c.postJSON(ctx, endpoint, payload)
}

func (c *Client) postJSON(ctx context.Context, endpoint string, body interface{}) error {
	return c.doJSON(ctx, c.httpClient, http.MethodPost, endpoint, body, nil)
}

func (c *Client) doJSON(ctx context.Context, httpCli *http.Client, method, endpoint string, body, out interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return err
	}
	// MAX requires Authorization: <MAX_BOT_TOKEN> without Bearer prefix or query param.
	req.Header.Set("Authorization", c.cfg.MaxBotToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	cli := httpCli
	if cli == nil {
		cli = c.httpClient
	}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("MAX API %s %s status %d: %s", method, endpoint, resp.StatusCode, string(respBytes))
	}
	if out != nil {
		if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
			return fmt.Errorf("decode MAX API %s response: %w", endpoint, err)
		}
	}
	return nil
}

func (c *Client) recordMock(r SentRecord) {
	c.mockMu.Lock()
	defer c.mockMu.Unlock()
	c.sent = append(c.sent, r)
	if len(c.sent) > 100 {
		c.sent = c.sent[len(c.sent)-100:]
	}
}

// RecentSent returns recently recorded outbound mock messages.
func (c *Client) RecentSent(userID string) []SentRecord {
	c.mockMu.Lock()
	defer c.mockMu.Unlock()
	var out []SentRecord
	for _, r := range c.sent {
		if userID == "" || r.UserID == userID || r.UserID == "" {
			out = append(out, r)
		}
	}
	return out
}

func countButtons(msg OutgoingMessage) int {
	n := 0
	for _, att := range msg.Attachments {
		for _, row := range att.Payload.Buttons {
			n += len(row)
		}
	}
	return n
}

// InlineKeyboard builds a MAX attachment for inline buttons.
func InlineKeyboard(rows ...[]Button) []Attachment {
	return []Attachment{
		{
			Type:    "inline_keyboard",
			Payload: InlineKeyboardPayload{Buttons: rows},
		},
	}
}
