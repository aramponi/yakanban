package main

import (
	"log"
	"net/http"
	"os"

	"github.com/aramponi/acme-api/internal/auth"
	"github.com/aramponi/acme-api/internal/config"
)

func main() {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	store := auth.NewStore()
	http.Handle("/me", auth.Handler(store))
	log.Printf("listening on %s (%s storage)", cfg.Addr, cfg.Provider)
	log.Fatal(http.ListenAndServe(cfg.Addr, nil))
}
