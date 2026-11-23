// Package contract holds the shapes of the Nutreek store plugin contract: what a plugin
// answers to search, product and availability calls. Nutreek's own description of it is
// docs/store-plugins.md in https://github.com/easly1989/nutreek.
//
//	GET /search?q=text          -> {"products": [Product, ...]}
//	GET /product?id=ID          -> Product
//	GET /availability?id=A&id=B -> {"products": [Status, ...]}
//
// Nutrition is per 100 g (or ml) and needs the four macros; a product without them cannot
// become an ingredient, so a search leaves it out.
package contract

import (
	"encoding/json"
	"net/http"
)

// Size is the package's size: Unit is "g", "ml" or "pz".
type Size struct {
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
}

// Nutrition is per 100 g (or ml). The four macros are required.
type Nutrition struct {
	Kcal         float64  `json:"kcal"`
	Protein      float64  `json:"protein"`
	Carbs        float64  `json:"carbs"`
	Fat          float64  `json:"fat"`
	Fiber        *float64 `json:"fiber,omitempty"`
	Sugars       *float64 `json:"sugars,omitempty"`
	SaturatedFat *float64 `json:"saturated_fat,omitempty"`
	Sodium       *float64 `json:"sodium,omitempty"`
}

// Product is one article of a store.
type Product struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Brand       string     `json:"brand,omitempty"`
	Size        Size       `json:"size"`
	Price       *float64   `json:"price,omitempty"`
	Currency    string     `json:"currency,omitempty"`
	Available   bool       `json:"available"`
	UnitWeightG *float64   `json:"unit_weight_g,omitempty"`
	Nutrition   *Nutrition `json:"nutrition,omitempty"`
}

// Status is the answer of an availability call about one product.
type Status struct {
	ID        string   `json:"id"`
	Price     *float64 `json:"price,omitempty"`
	Available bool     `json:"available"`
}

// WriteJSON answers with v as JSON.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError answers with {"error": msg}.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}
