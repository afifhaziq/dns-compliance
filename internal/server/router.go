package server

import (
	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/favicon"
	"github.com/afif/dns-tracking/internal/whois"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// whoisFetch and faviconFetch may be nil to disable the lazy on-watchlist-add
// WHOIS fetch / on-demand favicon fetch (tests pass nil so they never hit the
// network).
func RegisterRoutes(r chi.Router, store db.Store, scanner *Scanner, broadcaster *Broadcaster, cookieSecure bool, whoisFetch whois.Fetcher, faviconFetch favicon.Fetcher) {
	h := NewHandlers(store, scanner, broadcaster, whoisFetch, faviconFetch)
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
			r.Patch("/urls/{id}", h.ToggleURL)

			// DNS servers are global/shared — every authenticated role can
			// view them (results reference them by name); only mutating the
			// set is admin-only, gated below.
			r.Get("/dns-servers", h.ListDNSServers)

			r.Post("/scan", h.TriggerScan)
			r.Get("/scan/status", h.ScanStatus)
			r.Get("/scan/progress", h.ScanProgress)
			r.Get("/scan/progress/stream", h.ScanProgressStream)

			r.Get("/results", h.LatestResults)
			r.Get("/results/*", h.ResultsByURL)
			r.Get("/heatmap/*", h.HeatmapByURL)
			r.Get("/dns-records/*", h.DNSRecordsByURL)
			r.Get("/favicon/*", h.FaviconByURL)
			r.Get("/domain/*", h.DomainInfoByURL)
			r.Post("/domain/*", h.RefreshDomainInfo)
			r.Get("/isps/{isp}", h.ISPStats)
			r.Get("/isps/{isp}/trend", h.ISPTrend)
			r.Get("/isps/{isp}/timing", h.ISPTiming)
			r.Get("/trend", h.NationalTrend)

			r.Post("/screenshot", h.TriggerScreenshot)

			r.Group(func(r chi.Router) {
				r.Use(requireAdmin)

				r.Post("/dns-servers", h.CreateDNSServer)
				r.Delete("/dns-servers/{id}", h.DeleteDNSServer)

				r.Get("/admin/departments", h.ListDepartments)
				r.Post("/admin/departments", h.CreateDepartment)
				r.Get("/admin/users", h.ListUsers)
				r.Post("/admin/users", h.CreateUser)
				r.Delete("/admin/users/{id}", h.DeleteUser)
				r.Get("/admin/urls/unassigned", h.ListUnassignedURLs)
				r.Delete("/admin/urls/{id}", h.PurgeURL)

				r.Get("/admin/compliant-ips", h.ListCompliantIPs)
				r.Post("/admin/compliant-ips", h.CreateCompliantIP)
				r.Delete("/admin/compliant-ips/{id}", h.DeleteCompliantIP)
			})
		})
	})
}
