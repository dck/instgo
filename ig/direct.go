package ig

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/url"
	"regexp"
	"strconv"
)

type Inbox struct {
	Threads      []Thread
	OldestCursor string
	HasOlder     bool
}

func (c *Client) Inbox(ctx context.Context, cursor string) (*Inbox, error) {
	q := url.Values{
		"visual_message_return_type": {"unseen"},
		"thread_message_limit":       {"10"},
		"persistentBadging":          {"true"},
		"limit":                      {"20"},
		"is_prefetching":             {"false"},
	}
	if cursor != "" {
		q.Set("cursor", cursor)
		q.Set("direction", "older")
		q.Set("fetch_reason", "page_scroll")
	}
	body, err := c.get(ctx, "direct_v2/inbox/", q)
	if err != nil {
		return nil, err
	}
	var r struct {
		Inbox struct {
			Threads      []Thread `json:"threads"`
			OldestCursor string   `json:"oldest_cursor"`
			HasOlder     bool     `json:"has_older"`
		} `json:"inbox"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	return &Inbox{Threads: r.Inbox.Threads, OldestCursor: r.Inbox.OldestCursor, HasOlder: r.Inbox.HasOlder}, nil
}

func (c *Client) Thread(ctx context.Context, threadID, cursor string) (*Thread, error) {
	q := url.Values{
		"visual_message_return_type": {"unseen"},
		"direction":                  {"older"},
		"seq_id":                     {"40065"},
		"limit":                      {"20"},
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	body, err := c.get(ctx, "direct_v2/threads/"+threadID+"/", q)
	if err != nil {
		return nil, err
	}
	var r struct {
		Thread Thread `json:"thread"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	return &r.Thread, nil
}

var urlPattern = regexp.MustCompile(`https?://[^\s]+`)

func NewClientContext() string {
	return strconv.FormatUint(6800011111111111111+rand.Uint64N(88888888888888889), 10)
}

func (c *Client) SendText(ctx context.Context, threadID, text, clientContext string) (*Item, error) {
	form := url.Values{
		"action":                 {"send_item"},
		"is_x_transport_forward": {"false"},
		"send_silently":          {"false"},
		"is_shh_mode":            {"0"},
		"send_attribution":       {"message_button"},
		"client_context":         {clientContext},
		"device_id":              {c.s.AndroidDeviceID},
		"mutation_token":         {clientContext},
		"_uuid":                  {c.s.UUID},
		"btt_dual_send":          {"false"},
		"nav_chain":              {"1qT:feed_timeline:1,1qT:feed_timeline:2,1qT:feed_timeline:3,7Az:direct_inbox:4,7Az:direct_inbox:5,5rG:direct_thread:7"},
		"is_ae_dual_send":        {"false"},
		"offline_threading_id":   {clientContext},
		"thread_ids":             {"[" + threadID + "]"},
	}
	method := "text"
	if urls := urlPattern.FindAllString(text, -1); len(urls) > 0 {
		method = "link"
		raw, _ := json.Marshal(urls)
		form.Set("link_text", text)
		form.Set("link_urls", string(raw))
	} else {
		form.Set("text", text)
	}
	body, err := c.postForm(ctx, "direct_v2/threads/broadcast/"+method+"/", form)
	if err != nil {
		return nil, err
	}
	var r struct {
		Payload struct {
			ItemID        string `json:"item_id"`
			ClientContext string `json:"client_context"`
			Timestamp     Micros `json:"timestamp"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	return &Item{
		ItemID:        r.Payload.ItemID,
		UserID:        FlexString(c.UserID()),
		Timestamp:     r.Payload.Timestamp,
		ItemType:      "text",
		ClientContext: clientContext,
		Text:          text,
	}, nil
}

func (c *Client) MarkSeen(ctx context.Context, threadID, itemID string) error {
	token := NewClientContext()
	_, err := c.postForm(ctx, "direct_v2/threads/"+threadID+"/items/"+itemID+"/seen/", url.Values{
		"thread_id":            {threadID},
		"action":               {"mark_seen"},
		"client_context":       {token},
		"_uuid":                {c.s.UUID},
		"offline_threading_id": {token},
	})
	return err
}
