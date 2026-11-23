package conad

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/easly1989/nutreek-store-plugins/contract"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseSize(t *testing.T) {
	for _, c := range []struct {
		name string
		qty  float64
		unit string
		ok   bool
	}{
		{"Passata di Pomodoro Classica 700 g Conad", 700, "g", true},
		{"Passata di Pomodoro Classica 3 x 200 g Conad", 600, "g", true},
		{"100% Ananas 1 L Conad", 1000, "ml", true},
		{"Acqua Minerale Effervescente Naturale 6 x 1,5 l Conad", 9000, "ml", true},
		{"11 Paralleli Bionda 33 cl", 330, "ml", true},
		{"Riso Basmati 1 Kg", 1000, "g", true},
		{"Burro Chiarificato 200 g", 200, "g", true},
		{"Stuzzicadenti in barattolo", 0, "", false},
	} {
		q, u, ok := parseSize(c.name)
		if ok != c.ok || q != c.qty || u != c.unit {
			t.Errorf("parseSize(%q) = %v %q %v, want %v %q %v", c.name, q, u, ok, c.qty, c.unit, c.ok)
		}
	}
}

func TestParseProduct(t *testing.T) {
	p, err := parseProduct(fixture(t, "product-passata.html"))
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "109725" || p.Name != "Passata di Pomodoro Classica 700 g Conad" || p.Brand != "Conad" || p.Currency != "EUR" || !p.Available {
		t.Errorf("product = %+v", p.Product)
	}
	if p.Price == nil || *p.Price != 0.85 || p.Size.Quantity != 700 || p.Size.Unit != "g" {
		t.Errorf("price/size = %v %+v", p.Price, p.Size)
	}
	n := p.Nutrition
	if !p.hasNutrition || n.Kcal != 28 || n.Fat != 0.2 || n.Carbs != 4.1 || n.Protein != 1.8 ||
		n.Fiber == nil || *n.Fiber != 1.4 || n.Sugars == nil || *n.Sugars != 3.5 || n.SaturatedFat == nil || *n.SaturatedFat != 0.1 {
		t.Errorf("nutrition = %+v", n)
	}
	if out, _ := parseProduct(fixture(t, "product-passata-out.html")); out.Available {
		t.Error("an out-of-stock product is shown as available")
	}
	if none, err := parseProduct(fixture(t, "product-no-nutrition.html")); err != nil || none.hasNutrition || none.Nutrition != nil {
		t.Errorf("a page without the table: %+v, %v", none, err)
	}
	if multi, _ := parseProduct(fixture(t, "product-multipack.html")); multi.Size.Quantity != 600 {
		t.Errorf("3 x 200 g = %v", multi.Size)
	}
	if _, err := parseProduct("<html>not a product</html>"); err == nil {
		t.Error("a page with no product must fail")
	}
}

func TestParseSearch(t *testing.T) {
	cards := parseSearch(fixture(t, "search.html"))
	if len(cards) != 3 {
		t.Fatalf("%d cards: %+v", len(cards), cards)
	}
	if c := cards[0]; c.ID != "109725" || c.Title != "Passata di Pomodoro Classica 700 g Conad" || c.Price == nil || *c.Price != 0.85 {
		t.Errorf("first card = %+v (price %v)", c, c.Price)
	}
	for _, c := range cards {
		if c.ID == "" || c.Title == "" {
			t.Errorf("card without id or title: %+v", c)
		}
	}
}

// shop is a stand-in for the Conad site: a search page, and a product page for every code,
// reached through a redirect to its «canonical» address as on the real site.
func shop(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	pages := map[string]string{
		"109725": fixture(t, "product-passata.html"),
		"346539": fixture(t, "product-multipack.html"),
		"555555": fixture(t, "product-no-nutrition.html"),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch {
		case r.URL.Path == "/search":
			_, _ = w.Write([]byte(fixture(t, "search.html")))
		case strings.HasPrefix(r.URL.Path, "/p/x--"):
			http.Redirect(w, r, "/p/canonical--"+strings.TrimPrefix(r.URL.Path, "/p/x--"), http.StatusMovedPermanently)
		case strings.HasPrefix(r.URL.Path, "/p/canonical--"):
			if page, ok := pages[strings.TrimPrefix(r.URL.Path, "/p/canonical--")]; ok {
				_, _ = w.Write([]byte(page))
				return
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func newStore(t *testing.T, base string) *Store {
	t.Helper()
	s, err := New(Options{BaseURL: base, RequestsPerSecond: 1000})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func get(t *testing.T, h http.Handler, target string, out any) int {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	if out != nil {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatalf("%s: %v\n%s", target, err, w.Body.String())
		}
	}
	return w.Code
}

func TestContract(t *testing.T) {
	srv, hits := shop(t)
	h := newStore(t, srv.URL).Handler()

	// search: the products of the page that have a nutrition table (only 109725 and 346539 do in this shop)
	var res struct {
		Products []contract.Product `json:"products"`
	}
	if code := get(t, h, "/search?q=passata", &res); code != http.StatusOK {
		t.Fatalf("search: %d", code)
	}
	if len(res.Products) != 1 || res.Products[0].ID != "109725" || res.Products[0].Nutrition == nil || res.Products[0].Nutrition.Kcal != 28 {
		t.Errorf("search = %+v", res.Products)
	}
	// a second search for the same words reads nothing more
	before := hits.Load()
	get(t, h, "/search?q=Passata", &res)
	if hits.Load() != before {
		t.Errorf("a repeated search went to the shop again: %d requests", hits.Load()-before)
	}
	if code := get(t, h, "/search?q=", &res); code != http.StatusOK || len(res.Products) != 0 {
		t.Errorf("an empty search = %d, %d products", code, len(res.Products))
	}

	// product: by code, through the redirect
	var p contract.Product
	if code := get(t, h, "/product?id=109725", &p); code != http.StatusOK || p.ID != "109725" || p.Price == nil || *p.Price != 0.85 {
		t.Errorf("product = %d %+v", code, p)
	}
	for _, bad := range []string{"/product?id=999999", "/product?id=abc", "/product?id=", "/product?id=../../etc"} {
		if code := get(t, h, bad, nil); code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", bad, code)
		}
	}

	// availability: the known codes, the unknown ones left out
	var st struct {
		Products []contract.Status `json:"products"`
	}
	get(t, h, "/availability?id=109725&id=346539&id=999999", &st)
	if len(st.Products) != 2 || st.Products[0].ID != "109725" || !st.Products[0].Available {
		t.Errorf("availability = %+v", st.Products)
	}
}

func TestShopDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusServiceUnavailable) }))
	defer srv.Close()
	h := newStore(t, srv.URL).Handler()
	if code := get(t, h, "/search?q=pasta", nil); code != http.StatusBadGateway {
		t.Errorf("search with the shop down = %d, want 502", code)
	}
	if code := get(t, h, "/product?id=109725", nil); code != http.StatusBadGateway {
		t.Errorf("product with the shop down = %d, want 502", code)
	}
}

func TestRedirectToAnotherHostIsRefused(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("the other host was called") }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/x", http.StatusFound)
	}))
	defer srv.Close()
	if code := get(t, newStore(t, srv.URL).Handler(), "/product?id=109725", nil); code != http.StatusBadGateway {
		t.Errorf("a redirect to another host = %d, want 502", code)
	}
}

func TestLimiterSpacesRequests(t *testing.T) {
	l := newLimiter(20 * time.Millisecond)
	start := time.Now()
	for i := 0; i < 4; i++ {
		if err := l.wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(start); d < 55*time.Millisecond {
		t.Errorf("4 requests took %v, want at least 3 gaps", d)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	slow := newLimiter(time.Hour)
	_ = slow.wait(context.Background())
	if err := slow.wait(ctx); err == nil {
		t.Error("a cancelled wait must return an error")
	}
}
