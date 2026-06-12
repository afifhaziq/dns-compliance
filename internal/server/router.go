package server

import (
	"github.com/afif/dns-tracking/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func RegisterRoutes(r chi.Router, store db.Store, scanner *Scanner) {
	h := NewHandlers(store, scanner)

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Get("/urls", h.ListURLs)
		r.Post("/urls", h.CreateURL)
		r.Delete("/urls/{id}", h.DeleteURL)

		r.Get("/dns-servers", h.ListDNSServers)
		r.Post("/dns-servers", h.CreateDNSServer)
		r.Delete("/dns-servers/{id}", h.DeleteDNSServer)

		r.Post("/scan", h.TriggerScan)
		r.Get("/scan/status", h.ScanStatus)
		r.Get("/scan/progress", h.ScanProgress)

		r.Get("/results", h.LatestResults)
		r.Get("/results/*", h.ResultsByURL)

		r.Post("/screenshot", h.TriggerScreenshot)
	})
}
