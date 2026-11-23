//go:build live

package conad

import (
	"context"
	"testing"
	"time"
)

// go test -tags live ./conad -run Live -v: reads the real shop, a handful of pages.
func TestLive(t *testing.T) {
	s, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	page, err := s.get(ctx, "/search?query=passata")
	if err != nil {
		t.Fatal(err)
	}
	cards := parseSearch(page)
	t.Logf("%d cards", len(cards))
	for _, c := range cards[:min(3, len(cards))] {
		t.Logf("card %+v price %v", c, c.Price)
		pg, err := s.get(ctx, "/p/x--"+c.ID)
		if err != nil {
			t.Fatal(err)
		}
		p, err := parseProduct(pg)
		t.Logf("product %+v err %v nutrition %v", p.Product, err, p.hasNutrition)
	}
}
