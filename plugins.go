// Package storeplugins is the set of store plugins that come with Nutreek's «integrated
// stores»: each reads one store's public website and answers Nutreek's plugin contract
// (package contract). Nutreek mounts their handlers in its own process; the same handlers
// can run as an HTTP service with cmd/nutreek-store-plugins.
//
// A plugin is not an agreement with its store. It reads what the store shows to anyone, and
// stops if the store asks it to; whoever runs it answers for how they use it.
package storeplugins

import (
	"net/http"
	"sort"

	"github.com/easly1989/nutreek-store-plugins/conad"
)

// Plugin is one store.
type Plugin struct {
	ID          string // short and stable, in lowercase: «conad»
	Name        string // «Conad»
	Description string // one sentence, in English
	Site        string // the store's website
	// Handler builds the plugin's HTTP handler (see package contract).
	Handler func() (http.Handler, error)
}

var plugins = []Plugin{
	{
		ID: "conad", Name: "Conad", Site: "https://spesaonline.conad.it",
		Description: "Products, prices and nutrition from Conad's online shop. Prices are those of the site's default store.",
		Handler: func() (http.Handler, error) {
			s, err := conad.New(conad.Options{})
			if err != nil {
				return nil, err
			}
			return s.Handler(), nil
		},
	},
}

// All returns the plugins, sorted by ID.
func All() []Plugin {
	out := append([]Plugin(nil), plugins...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get returns the plugin with that ID.
func Get(id string) (Plugin, bool) {
	for _, p := range plugins {
		if p.ID == id {
			return p, true
		}
	}
	return Plugin{}, false
}
