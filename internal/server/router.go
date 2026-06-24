package server

import (
	"github.com/afif/dns-tracking/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func RegisterRoutes(r chi.Router, store db.Store, scanner *Scanner, broadcaster *Broadcaster, cookieSecure bool) {
	h := NewHandlers(store, scanner, broadcaster)
	ah := NewAuthHandlers(store, cookieSecure)

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Post("/auth/login", ah.Login) // public

		r.Group(func(r chi.Router) {
			r.Use(requireAuth(store))

			r.Post("/auth/logout", ah.Logout)
			r.Get("/auth/me", ah.Me)

			r.Get("/urls", h.ListURLs)
			r.Post("/urls", h.AddToWatchlist)
			r.Delete("/urls/{id}", h.RemoveFromWatchlist)

			r.Post("/scan", h.TriggerScan)
			r.Get("/scan/status", h.ScanStatus)
			r.Get("/scan/progress", h.ScanProgress)
			r.Get("/scan/progress/stream", h.ScanProgressStream)

			r.Get("/results", h.LatestResults)
			r.Get("/results/*", h.ResultsByURL)
			r.Get("/heatmap/*", h.HeatmapByURL)
			r.Get("/dns-records/*", h.DNSRecordsByURL)

			r.Post("/screenshot", h.TriggerScreenshot)

			r.Group(func(r chi.Router) {
				r.Use(requireAdmin)

				r.Get("/dns-servers", h.ListDNSServers)
				r.Post("/dns-servers", h.CreateDNSServer)
				r.Delete("/dns-servers/{id}", h.DeleteDNSServer)

				r.Get("/admin/departments", h.ListDepartments)
				r.Post("/admin/departments", h.CreateDepartment)
				r.Get("/admin/users", h.ListUsers)
				r.Post("/admin/users", h.CreateUser)
				r.Delete("/admin/users/{id}", h.DeleteUser)
				r.Get("/admin/urls/unassigned", h.ListUnassignedURLs)
				r.Delete("/admin/urls/{id}", h.PurgeURL)
			})
		})
	})
}
