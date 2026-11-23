// Command nutreek-store-plugins runs the store plugins as an HTTP service, for a Nutreek
// that connects them by address instead of using its integrated stores:
//
//	nutreek-store-plugins -addr 127.0.0.1:8110 -token SECRET
//
// serves every plugin under its ID: http://127.0.0.1:8110/conad/search?q=pasta, with the
// token as «Authorization: Bearer SECRET».
package main

import (
	"crypto/subtle"
	"flag"
	"log"
	"net/http"
	"strings"
	"time"

	storeplugins "github.com/easly1989/nutreek-store-plugins"
	"github.com/easly1989/nutreek-store-plugins/contract"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8110", "address to listen on")
	token := flag.String("token", "", "bearer token the callers must send (required)")
	flag.Parse()
	if len(*token) < 8 {
		log.Fatal("-token is required (at least 8 characters)")
	}
	mux := http.NewServeMux()
	for _, p := range storeplugins.All() {
		h, err := p.Handler()
		if err != nil {
			log.Fatalf("%s: %v", p.ID, err)
		}
		mux.Handle("/"+p.ID+"/", http.StripPrefix("/"+p.ID, h))
	}
	srv := &http.Server{
		Addr: *addr, Handler: auth(*token, mux),
		ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 15 * time.Second,
	}
	log.Printf("serving %d plugins on %s", len(storeplugins.All()), *addr)
	log.Fatal(srv.ListenAndServe())
}

// auth lets through only the requests that carry the bearer token.
func auth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			contract.WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}
