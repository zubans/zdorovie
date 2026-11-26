package maxbot

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"time"
)

// BotProfile describes the authenticated MAX bot returned by GET /me.
type BotProfile struct {
	UserID           int64  `json:"user_id"`
	FirstName        string `json:"first_name"`
	LastName         string `json:"last_name,omitempty"`
	Username         string `json:"username"`
	IsBot            bool   `json:"is_bot"`
	LastActivityTime int64  `json:"last_activity_time,omitempty"`
	Description      string `json:"description,omitempty"`
}

// UpdatesResponse models the long-polling response from GET /updates.
type UpdatesResponse struct {
	Updates []WebhookEvent `json:"updates"`
	Marker  *int64         `json:"marker,omitempty"`
}

// Subscription describes an active MAX webhook subscription.
type Subscription struct {
	URL         string   `json:"url"`
	Time        int64    `json:"time,omitempty"`
	UpdateTypes []string `json:"update_types,omitempty"`
}

type subscriptionsResponse struct {
	Subscriptions []Subscription `json:"subscriptions"`
}

// GetMe verifies the bot token and returns the authenticated bot profile (GET /me).
func (c *Client) GetMe(ctx context.Context) (BotProfile, error) {
	if !c.IsLive() {
		return BotProfile{
			FirstName: "MockBot",
			Username:  "mock_max_bot",
			IsBot:     true,
		}, nil
	}
	if err := c.globalLim.Wait(ctx); err != nil {
		return BotProfile{}, err
	}
	var out BotProfile
	endpoint := fmt.Sprintf("%s/me", c.baseURL())
	if err := c.doJSON(ctx, c.httpClient, "GET", endpoint, nil, &out); err != nil {
		return BotProfile{}, err
	}
	return out, nil
}

// GetSubscriptions returns active webhook subscriptions registered on the MAX bot (GET /subscriptions).
func (c *Client) GetSubscriptions(ctx context.Context) ([]Subscription, error) {
	if !c.IsLive() {
		return nil, nil
	}
	if err := c.globalLim.Wait(ctx); err != nil {
		return nil, err
	}
	var out subscriptionsResponse
	endpoint := fmt.Sprintf("%s/subscriptions", c.baseURL())
	if err := c.doJSON(ctx, c.httpClient, "GET", endpoint, nil, &out); err != nil {
		return nil, err
	}
	return out.Subscriptions, nil
}

// GetUpdates fetches queued bot events from MAX Bot API via long polling (GET /updates).
func (c *Client) GetUpdates(ctx context.Context, marker *int64, timeoutSec, limit int) (UpdatesResponse, error) {
	if !c.IsLive() {
		return UpdatesResponse{}, nil
	}
	if err := c.globalLim.Wait(ctx); err != nil {
		return UpdatesResponse{}, err
	}
	if timeoutSec < 0 {
		timeoutSec = 0
	} else if timeoutSec > 90 {
		timeoutSec = 90
	}
	if limit <= 0 {
		limit = 50
	} else if limit > 1000 {
		limit = 1000
	}

	params := url.Values{}
	params.Set("timeout", strconv.Itoa(timeoutSec))
	params.Set("limit", strconv.Itoa(limit))
	if marker != nil {
		params.Set("marker", strconv.FormatInt(*marker, 10))
	}

	endpoint := fmt.Sprintf("%s/updates?%s", c.baseURL(), params.Encode())
	var out UpdatesResponse
	if err := c.doJSON(ctx, c.pollHTTPClient, "GET", endpoint, nil, &out); err != nil {
		return UpdatesResponse{}, err
	}
	return out, nil
}

// StartPolling runs the blocking MAX long-polling loop until ctx is canceled.
func (c *Client) StartPolling(ctx context.Context, engine *Engine) {
	if !c.IsLive() || engine == nil {
		return
	}

	if subs, err := c.GetSubscriptions(ctx); err == nil && len(subs) > 0 {
		log.Printf("WARNING: MAX bot has %d active webhook subscription(s); GET /updates polling may not receive events while webhooks are active", len(subs))
	}

	var marker *int64
	consecutiveErrs := 0

	for {
		if ctx.Err() != nil {
			return
		}

		resp, err := c.GetUpdates(ctx, marker, 25, 50)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			consecutiveErrs++
			backoff := 2 * time.Second << min(consecutiveErrs-1, 4)
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			log.Printf("[MAX Poll] GET /updates failed (retry in %s): %v", backoff, err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			continue
		}

		consecutiveErrs = 0
		if resp.Marker != nil {
			next := *resp.Marker
			marker = &next
		}

		for _, ev := range resp.Updates {
			if ev.UpdateType == "" {
				continue
			}
			evCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			if handleErr := engine.HandleEvent(evCtx, ev); handleErr != nil {
				log.Printf("[MAX Poll] handle update_type=%s failed: %v", ev.UpdateType, handleErr)
			}
			cancel()
		}

		if len(resp.Updates) > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
}
