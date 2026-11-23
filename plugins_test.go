package storeplugins

import (
	"net/http/httptest"
	"testing"
)

func TestRegistry(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("no plugins")
	}
	seen := map[string]bool{}
	for _, p := range all {
		if p.ID == "" || p.Name == "" || p.Description == "" || p.Site == "" || p.Handler == nil || seen[p.ID] {
			t.Errorf("bad plugin %+v", p)
		}
		seen[p.ID] = true
		h, err := p.Handler()
		if err != nil || h == nil {
			t.Errorf("%s: handler %v, %v", p.ID, h, err)
		}
		if got, ok := Get(p.ID); !ok || got.ID != p.ID {
			t.Errorf("Get(%q) failed", p.ID)
		}
	}
	if _, ok := Get("nope"); ok {
		t.Error("Get found a plugin that does not exist")
	}
	h, _ := all[0].Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/product?id=abc", nil))
	if w.Code != 404 {
		t.Errorf("the handler answered %d to a bad id", w.Code)
	}
}
