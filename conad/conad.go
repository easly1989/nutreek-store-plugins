// Package conad is a Nutreek store plugin for Conad's online shop, https://spesaonline.conad.it.
//
// It reads the shop's public pages, the way a browser does, with plain HTTP: the search page
// for the list of products, a product page for its price, stock and nutrition table. It sends
// at most a few requests a second, keeps what it read for a while, and never tries to look
// like a browser or to get past anything the site uses to tell programs from people: if the
// site starts refusing it, it answers with an error and stops.
//
// The prices are those the site shows to a visitor who has chosen no store; they can differ
// from the ones of a given shop, so a user of them should present them as indicative.
package conad

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/easly1989/nutreek-store-plugins/contract"
)

// DefaultBase is the shop's address.
const DefaultBase = "https://spesaonline.conad.it"

const (
	maxPage     = 3 << 20 // a page is read up to 3 MiB
	maxResults  = 8       // products a search returns
	maxIDs      = 50      // products an availability call looks at (as many as Nutreek asks)
	concurrency = 3       // pages fetched at once
	missingTTL  = 2 * time.Minute
)

// Options tunes a Store; the zero value is what production uses.
type Options struct {
	BaseURL           string        // the shop's address, DefaultBase
	Client            *http.Client  // the HTTP client; a plain one with a 10 s timeout
	RequestsPerSecond float64       // the most requests a second, 4
	ProductTTL        time.Duration // how long a product page is kept, 30 minutes
	SearchTTL         time.Duration // how long a search is kept, 10 minutes
}

// Store reads Conad's shop.
type Store struct {
	base      *url.URL
	hc        *http.Client
	lim       *limiter
	products  *cache[product]
	searches  *cache[[]card]
	userAgent string
}

// New builds a Store.
func New(o Options) (*Store, error) {
	if o.BaseURL == "" {
		o.BaseURL = DefaultBase
	}
	base, err := url.Parse(strings.TrimRight(o.BaseURL, "/"))
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") {
		return nil, fmt.Errorf("conad: bad base URL %q", o.BaseURL)
	}
	if o.RequestsPerSecond <= 0 {
		o.RequestsPerSecond = 4
	}
	if o.ProductTTL <= 0 {
		o.ProductTTL = 30 * time.Minute
	}
	if o.SearchTTL <= 0 {
		o.SearchTTL = 10 * time.Minute
	}
	hc := o.Client
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	// A redirect stays on the shop: a product's address is rewritten to its canonical one.
	cp := *hc
	cp.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("conad: too many redirects")
		}
		if req.URL.Host != base.Host {
			return fmt.Errorf("conad: redirect to another host %s", req.URL.Host)
		}
		// The shop answers 406 to a product page asked with a Referer, which net/http adds
		// when it follows a redirect: it is asked the way a first visit would.
		req.Header.Del("Referer")
		return nil
	}
	return &Store{
		base: base, hc: &cp,
		lim:       newLimiter(time.Duration(float64(time.Second) / o.RequestsPerSecond)),
		products:  newCache[product](o.ProductTTL, 500),
		searches:  newCache[[]card](o.SearchTTL, 100),
		userAgent: "NutreekStorePlugins/0.1 (+https://github.com/easly1989/nutreek-store-plugins)",
	}, nil
}

// get reads a page of the shop.
func (s *Store) get(ctx context.Context, path string) (string, error) {
	if err := s.lim.wait(ctx); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base.String()+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", s.userAgent)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "it-IT,it;q=0.9")
	resp, err := s.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("conad: %s answered %d", path, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxPage+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxPage {
		return "", fmt.Errorf("conad: %s is larger than %d bytes", path, maxPage)
	}
	return string(b), nil
}

// errNotFound says the shop has no such product.
var errNotFound = errors.New("conad: no such product")

// product reads a product by its code. The shop sends any slug to the right page, so the
// code is all that is needed.
func (s *Store) product(ctx context.Context, id string) (product, error) {
	if _, err := strconv.ParseUint(id, 10, 64); err != nil || len(id) > 12 {
		return product{}, errNotFound
	}
	if p, ok := s.products.get(id); ok {
		if p.missing {
			return product{}, errNotFound
		}
		return p, nil
	}
	missing := func() (product, error) {
		// Not asked again for a short while: a page that is not a product may be a hiccup of
		// the site, and must not blank the product for as long as a good page is kept.
		s.products.putFor(id, product{missing: true}, missingTTL)
		return product{}, errNotFound
	}
	page, err := s.get(ctx, "/p/x--"+id)
	if errors.Is(err, errNotFound) {
		return missing()
	}
	if err != nil {
		return product{}, err
	}
	p, err := parseProduct(page)
	if err != nil || p.ID != id { // no product, or not the one asked for
		return missing()
	}
	s.products.put(id, p)
	return p, nil
}

// search lists the products matching q, with their nutrition; those whose page has none are
// left out, as they cannot become an ingredient.
func (s *Store) search(ctx context.Context, q string) ([]contract.Product, error) {
	q = strings.TrimSpace(q)
	if q == "" || len(q) > 100 {
		return nil, nil
	}
	cards, ok := s.searches.get(strings.ToLower(q))
	if !ok {
		page, err := s.get(ctx, "/search?query="+url.QueryEscape(q))
		if err != nil {
			return nil, err
		}
		cards = parseSearch(page)
		if len(cards) > maxResults {
			cards = cards[:maxResults]
		}
		s.searches.put(strings.ToLower(q), cards)
	}
	found := make([]*product, len(cards))
	s.each(ctx, len(cards), func(i int) {
		if p, err := s.product(ctx, cards[i].ID); err == nil {
			found[i] = &p
		}
	})
	if err := ctx.Err(); err != nil && allNil(found) {
		return nil, err
	}
	var out []contract.Product
	for _, p := range found {
		if p != nil && p.hasNutrition {
			out = append(out, p.Product)
		}
	}
	return out, nil
}

func allNil(ps []*product) bool {
	for _, p := range ps {
		if p != nil {
			return false
		}
	}
	return true
}

// status looks at the price and stock of products; those it cannot read are left out.
func (s *Store) status(ctx context.Context, ids []string) []contract.Status {
	if len(ids) > maxIDs {
		ids = ids[:maxIDs]
	}
	out := make([]*contract.Status, len(ids))
	s.each(ctx, len(ids), func(i int) {
		if p, err := s.product(ctx, ids[i]); err == nil {
			out[i] = &contract.Status{ID: p.ID, Price: p.Price, Available: p.Available}
		}
	})
	var res []contract.Status
	for _, st := range out {
		if st != nil {
			res = append(res, *st)
		}
	}
	return res
}

// each runs f(0..n-1), a few at a time; it stops starting new ones when ctx ends.
func (s *Store) each(ctx context.Context, n int, f func(i int)) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	for i := 0; i < n; i++ {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() {
				_ = recover() // a page the parser chokes on loses that product, not the process
				<-sem
				wg.Done()
			}()
			f(i)
		}()
	}
	wg.Wait()
}

// Handler answers the plugin contract (see package contract).
func (s *Store) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		products, err := s.search(ctx, r.URL.Query().Get("q"))
		if err != nil {
			contract.WriteError(w, http.StatusBadGateway, "the shop did not answer")
			return
		}
		if products == nil {
			products = []contract.Product{}
		}
		contract.WriteJSON(w, http.StatusOK, map[string]any{"products": products})
	})
	mux.HandleFunc("GET /product", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		p, err := s.product(ctx, r.URL.Query().Get("id"))
		switch {
		case errors.Is(err, errNotFound):
			contract.WriteError(w, http.StatusNotFound, "no such product")
		case err != nil:
			contract.WriteError(w, http.StatusBadGateway, "the shop did not answer")
		default:
			contract.WriteJSON(w, http.StatusOK, p.Product)
		}
	})
	mux.HandleFunc("GET /availability", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		st := s.status(ctx, r.URL.Query()["id"])
		if st == nil {
			st = []contract.Status{}
		}
		contract.WriteJSON(w, http.StatusOK, map[string]any{"products": st})
	})
	return mux
}
