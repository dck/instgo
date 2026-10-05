package tui

import (
	"testing"

	"instgo/ig"
)

func ids(items []ig.Item) string {
	s := ""
	for _, it := range items {
		s += it.ItemID
	}
	return s
}

func TestMergeItemsLatestDropsUnsentKeepsNewer(t *testing.T) {
	existing := []ig.Item{{ItemID: "a", Timestamp: 1}, {ItemID: "b", Timestamp: 2}, {ItemID: "x", Timestamp: 3}, {ItemID: "c", Timestamp: 4}, {ItemID: "s", Timestamp: 9}}
	page := []ig.Item{{ItemID: "b", Timestamp: 2}, {ItemID: "c", Timestamp: 4}, {ItemID: "d", Timestamp: 5}}
	if got := ids(mergeItems(existing, page, true)); got != "abcds" {
		t.Fatalf("got %s", got)
	}
}

func TestMergeItemsOlderPrepends(t *testing.T) {
	existing := []ig.Item{{ItemID: "c", Timestamp: 3}}
	page := []ig.Item{{ItemID: "a", Timestamp: 1}, {ItemID: "b", Timestamp: 2}}
	if got := ids(mergeItems(existing, page, false)); got != "abc" {
		t.Fatalf("got %s", got)
	}
}
