package conad

import (
	"encoding/json"
	"errors"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/easly1989/nutreek-store-plugins/contract"
)

// card is a product as a search page lists it.
type card struct {
	ID    string
	Title string
	Price *float64
}

var (
	cardID     = regexp.MustCompile(`href="/p/[^"]*--(\d+)"`)
	cardTitle  = regexp.MustCompile(`(?s)<h3[^>]*>(.*?)</h3>`)
	cardPrice  = regexp.MustCompile(`class="(?:[^"]*\s)?product-price(?:\s[^"]*)?"[^>]*>\s*([\d.]+),(\d{2})`)
	tags       = regexp.MustCompile(`(?s)<script.*?</script>|<style.*?</style>|<[^>]+>`)
	spaces     = regexp.MustCompile(`\s+`)
	h1         = regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`)
	jsonLD     = regexp.MustCompile(`(?s)<script[^>]*type="application/ld\+json"[^>]*>(.*?)</script>`)
	sizeInName = regexp.MustCompile(`(?i)(?:(\d+)\s*[x×]\s*)?(\d+(?:[.,]\d+)?)\s*(kg|g|ml|cl|lt|l)\b`)
	per100     = regexp.MustCompile(`per\s*100\s*(g|ml)`)
	kcal       = regexp.MustCompile(`([\d.,]+)\s*kcal`)
)

// text turns HTML into plain text: no scripts, styles or tags, entities decoded, one space
// between words.
func text(s string) string {
	return strings.TrimSpace(spaces.ReplaceAllString(html.UnescapeString(tags.ReplaceAllString(s, " ")), " "))
}

// parseSearch reads the products a search page lists, in page order, once each.
func parseSearch(page string) []card {
	idx := cardID.FindAllStringSubmatchIndex(page, -1)
	var out []card
	seen := map[string]bool{}
	for i, m := range idx {
		id := page[m[2]:m[3]]
		if seen[id] {
			continue
		}
		seen[id] = true
		end := len(page)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		chunk := page[m[0]:end]
		if len(chunk) > 8000 {
			chunk = chunk[:8000]
		}
		c := card{ID: id}
		if t := cardTitle.FindStringSubmatch(chunk); t != nil {
			c.Title = text(t[1])
		}
		if p := cardPrice.FindStringSubmatch(chunk); p != nil {
			c.Price = euros(p[1], p[2])
		}
		if c.Title != "" {
			out = append(out, c)
		}
	}
	return out
}

func euros(whole, cents string) *float64 {
	v, err := strconv.ParseFloat(strings.ReplaceAll(whole, ".", "")+"."+cents, 64)
	if err != nil {
		return nil
	}
	return &v
}

// number reads a number written the Italian way ("0,2", "1.250,5").
func number(s string) (float64, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ".", "")
	v, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
	return v, err == nil
}

// parseSize reads the package size out of a product's name: «700 g», «1,5 l», «50 cl»,
// «3 x 200 g». The last size in the name wins (a brand can have a number: «11 Paralleli»).
// The result is in g or ml; ok is false when there is none.
func parseSize(name string) (qty float64, unit string, ok bool) {
	all := sizeInName.FindAllStringSubmatch(name, -1)
	if all == nil {
		return 0, "", false
	}
	m := all[len(all)-1]
	q, good := number(m[2])
	if !good || q <= 0 {
		return 0, "", false
	}
	if m[1] != "" {
		n, _ := strconv.Atoi(m[1])
		if n > 1 {
			q *= float64(n)
		}
	}
	switch strings.ToLower(m[3]) {
	case "kg":
		return q * 1000, "g", true
	case "g":
		return q, "g", true
	case "l", "lt":
		return q * 1000, "ml", true
	case "cl":
		return q * 10, "ml", true
	}
	return q, "ml", true
}

// nutrientRe builds the pattern of a nutrient in the table's text: «Fibre 1,4g».
func nutrientRe(label string) *regexp.Regexp {
	return regexp.MustCompile(label + `\s+<?\s*([\d.,]+)\s*g\b`)
}

var (
	reFat    = nutrientRe(`\bGrassi`)
	reSat    = nutrientRe(`Acidi grassi saturi`)
	reCarbs  = nutrientRe(`\bCarboidrati`)
	reSugars = nutrientRe(`\bZuccheri`)
	reFiber  = nutrientRe(`\bFibre`)
	reProt   = nutrientRe(`\bProteine`)
)

// parseNutrition reads the «Valori nutrizionali» table of a product page. unit is the
// one the values are per 100 of ("g" or "ml"); ok is false without the four macros.
func parseNutrition(page string) (n contract.Nutrition, unit string, ok bool) {
	t := text(page)
	i := strings.Index(t, "Valori nutrizionali")
	if i < 0 {
		return n, "", false
	}
	t = t[i:]
	if j := strings.Index(t, "Tracciabilit"); j > 0 {
		t = t[:j]
	}
	unit = "g"
	if m := per100.FindStringSubmatch(t); m != nil {
		unit = m[1]
	}
	get := func(re *regexp.Regexp) (float64, bool) {
		m := re.FindStringSubmatch(t)
		if m == nil {
			return 0, false
		}
		return number(m[1])
	}
	opt := func(re *regexp.Regexp) *float64 {
		if v, ok := get(re); ok {
			return &v
		}
		return nil
	}
	k := kcal.FindStringSubmatch(t)
	if k == nil {
		return n, "", false
	}
	var okK, okP, okC, okF bool
	n.Kcal, okK = number(k[1])
	n.Protein, okP = get(reProt)
	n.Carbs, okC = get(reCarbs)
	n.Fat, okF = get(reFat)
	if !(okK && okP && okC && okF) {
		return n, "", false
	}
	n.Fiber, n.Sugars, n.SaturatedFat = opt(reFiber), opt(reSugars), opt(reSat)
	return n, unit, true
}

// product is what a product page tells.
type product struct {
	contract.Product
	hasNutrition bool
	missing      bool // the shop has no such product (kept so that it is not asked again)
}

var errNoProduct = errors.New("conad: the page has no product")

// parseProduct reads a product page: the schema.org data (code, price, stock), the name,
// the size in it and the nutrition table.
func parseProduct(page string) (product, error) {
	var ld struct {
		Type  string `json:"@type"`
		SKU   string `json:"sku"`
		Brand struct {
			Name string `json:"name"`
		} `json:"brand"`
		Offers struct {
			Price        string `json:"price"`
			Availability string `json:"availability"`
		} `json:"offers"`
	}
	found := false
	for _, m := range jsonLD.FindAllStringSubmatch(page, -1) {
		if !strings.Contains(m[1], `"Product"`) {
			continue
		}
		if err := json.Unmarshal([]byte(m[1]), &ld); err == nil && ld.Type == "Product" {
			found = true
			break
		}
	}
	var p product
	if !found || ld.SKU == "" {
		return p, errNoProduct
	}
	name := ""
	if m := h1.FindStringSubmatch(page); m != nil {
		name = text(m[1])
	}
	if name == "" {
		return p, errNoProduct
	}
	p.ID, p.Name, p.Currency = ld.SKU, name, "EUR"
	p.Brand = brandName(ld.Brand.Name)
	avail := strings.ToLower(ld.Offers.Availability)
	p.Available = !strings.Contains(avail, "outofstock") && !strings.Contains(avail, "discontinued") && !strings.Contains(avail, "soldout")
	if v, err := strconv.ParseFloat(ld.Offers.Price, 64); err == nil && v > 0 { // 0 means the shop sells it nowhere near the default store
		p.Price = &v
	}
	n, unit, ok := parseNutrition(page)
	if ok {
		p.Nutrition, p.hasNutrition = &n, true
	}
	if q, u, ok := parseSize(name); ok {
		p.Size = contract.Size{Quantity: q, Unit: u}
	} else {
		p.Size = contract.Size{Unit: unit}
	}
	return p, nil
}

// brandName turns «CONAD» into «Conad».
func brandName(s string) string {
	s = strings.TrimSpace(s)
	if s != "" && s == strings.ToUpper(s) {
		return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
	}
	return s
}
