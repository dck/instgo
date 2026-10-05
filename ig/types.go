package ig

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"
)

type FlexString string

func (f *FlexString) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = FlexString(s)
		return nil
	}
	*f = FlexString(b)
	return nil
}

type Micros int64

func (m *Micros) UnmarshalJSON(b []byte) error {
	var s FlexString
	if err := s.UnmarshalJSON(b); err != nil {
		return err
	}
	if s == "" {
		return nil
	}
	n, err := strconv.ParseInt(string(s), 10, 64)
	if err != nil {
		return err
	}
	*m = Micros(n)
	return nil
}

func (m Micros) Time() time.Time {
	return time.UnixMicro(int64(m))
}

type User struct {
	PK       FlexString `json:"pk"`
	Username string     `json:"username"`
	FullName string     `json:"full_name"`
}

type Item struct {
	ItemID        string     `json:"item_id"`
	UserID        FlexString `json:"user_id"`
	Timestamp     Micros     `json:"timestamp"`
	ItemType      string     `json:"item_type"`
	ClientContext string     `json:"client_context"`
	Text          string     `json:"text"`
	Like          string     `json:"like"`
	Link          *struct {
		Text string `json:"text"`
	} `json:"link"`
	ActionLog *struct {
		Description string `json:"description"`
	} `json:"action_log"`
	ReelShare *struct {
		Text string `json:"text"`
	} `json:"reel_share"`
	Media *struct {
		MediaType int `json:"media_type"`
	} `json:"media"`
	Placeholder *struct {
		Message string `json:"message"`
	} `json:"placeholder"`
	RepliedTo *Item `json:"replied_to_message"`
}

type LastSeen struct {
	ItemID    string `json:"item_id"`
	Timestamp Micros `json:"timestamp"`
}

type Thread struct {
	ThreadID       string              `json:"thread_id"`
	Title          string              `json:"thread_title"`
	IsGroup        bool                `json:"is_group"`
	Users          []User              `json:"users"`
	Items          []Item              `json:"items"`
	LastActivityAt Micros              `json:"last_activity_at"`
	LastSeenAt     map[string]LastSeen `json:"last_seen_at"`
	OldestCursor   string              `json:"oldest_cursor"`
	HasOlder       bool                `json:"has_older"`
}
