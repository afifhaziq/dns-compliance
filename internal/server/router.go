package server

import (
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/favicon"
	"github.com/afif/dns-tracking/internal/ipinfo"
	"github.com/afif/dns-tracking/internal/subfinder"
	"github.com/afif/dns-tracking/internal/whois"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/time/rate"
)

// whoisFetch, faviconFetch, subfinderFetch, ipFetch, and netnameFetch may be
// nil to disable the lazy on-watchlist-add WHOIS/subdomain fetch, on-demand
// favicon fetch, or on-demand hosting-info refresh (tests pass nil so they
// never hit the network or shell out).
func RegisterRoutes(r chi.Router, store db.Store, scanner *Scanner, broadcaster *Broadcaster, cookieSecure bool, whoisFetch whois.Fetcher, faviconFetch favicon.Fetcher, subfinderFetch subfinder.Fetcher, ipFetch ipinfo.Fetcher, netnameFetch whois.IPFetcher, notify dueDateRescheduler) {
	h := NewHandlers(store, scanner, broadcaster, whoisFetch, faviconFetch, subfinderFetch, ipFetch, netnameFetch, notify)
	ah := NewAuthHandlers(store, cookieSecure)

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	// Some JSON responses (case-summaries, case-letters at page_size=5000)
	// run into multiple MB uncompressed — gzip cuts that by ~90% over the
	// wire for effectively free, since every response already declares
	// Content-Type via writeJSON.
	r.Use(middleware.Compress(5, "application/json"))

	// 5 attempts/min per IP — slows brute-force/enumeration against login
	// without a noticeable effect on a real user occasionally mistyping.
	loginLimit := rateLimitByIP(rate.Every(12*time.Second), 5)
	// 1 trigger/2s (burst 3) per user — scan/screenshot are expensive
	// (spawn the crawler / headless Chrome); this just stops accidental or
	// scripted spam, not a real usage pattern.
	scanLimit := rateLimitByUser(rate.Every(2*time.Second), 3)

	r.Route("/api", func(r chi.Router) {
		r.Use(requireFetchHeader)

		r.With(loginLimit).Post("/auth/login", ah.Login) // public

		r.Group(func(r chi.Router) {
			r.Use(requireAuth(store))

			r.Post("/auth/logout", ah.Logout)
			r.Get("/auth/me", ah.Me)
			r.Post("/auth/change-password", ah.ChangePassword)

			r.Get("/urls", h.ListURLs)
			r.Post("/urls", h.AddToWatchlist)
			r.Delete("/urls/{id}", h.RemoveFromWatchlist)
			r.Patch("/urls/{id}", h.ToggleURL)
			r.Get("/urls/requested-count", h.URLsRequestedThisMonth)

			// Personal per-user data-grid layout (column visibility/sort/page
			// size) — no admin gate, scoped to the calling user via session.
			r.Get("/grid-preferences/{key}", h.GetGridPreference)
			r.Put("/grid-preferences/{key}", h.SaveGridPreference)

			// DNS servers are global/shared — every authenticated role can
			// view them (results reference them by name); only mutating the
			// set is admin-only, gated below.
			r.Get("/dns-servers", h.ListDNSServers)
			r.Get("/dns-servers/{id}/uptime", h.ServerUptime)

			// Agency and Department (read) are the same kind of shared/global
			// reference data — open to any authenticated role for the
			// Agency/Requesting-Dept dropdowns on the watchlist page; only
			// mutating agencies is admin-gated (below), and Department's own
			// mutation route stays super-admin-only, untouched.
			r.Get("/agencies", h.ListAgencies)
			// Recipient/Requestor dropdowns on the Docs page's Add Document
			// dialog — same read-open/write-admin-gated shape as Agency.
			r.Get("/recipients", h.ListRecipients)
			r.Get("/requestors", h.ListRequestors)
			r.Get("/departments", h.ListDepartmentsOpen)
			r.Get("/users/open", h.ListUsersOpen)
			r.Get("/due-date-presets", h.ListDueDatePresets)

			r.With(scanLimit).Post("/scan", h.TriggerScan)
			r.Post("/scan/cancel", h.CancelScan)
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
			r.Get("/subdomains/*", h.SubdomainsByURL)
			r.Post("/subdomains/*", h.RefreshSubdomains)
			r.Post("/hosting/{ip}", h.RefreshHostingInfo)
			r.Get("/isps/{isp}", h.ISPStats)
			r.Get("/isps/{isp}/trend", h.ISPTrend)
			r.Get("/isps/{isp}/timing", h.ISPTiming)
			r.Get("/trend", h.NationalTrend)
			r.Get("/resurfaced", h.ResurfacedDomains)
			r.Get("/notifications", h.ListNotifications)
			r.Get("/notifications/unread-count", h.UnreadNotificationCount)
			r.Patch("/notifications/{id}/read", h.MarkNotificationRead)
			r.Delete("/notifications/{id}", h.DeleteNotification)
			r.Delete("/notifications", h.ClearAllNotifications)
			r.Get("/domains", h.DomainSummaries)
			r.Get("/domains/*", h.DomainServerSummaries)
			r.Get("/isp-logos", h.ListISPLogos)

			r.With(scanLimit).Post("/screenshot", h.TriggerScreenshot)

			// Legal citation catalog — shared/global reference data, same
			// read-open/write-admin-gated shape as DNS servers/ISP logos
			// (mutation routes are in the requireAnyAdmin group below).
			r.Get("/legal/instruments", h.ListInstruments)
			r.Get("/legal/instruments/{id}/citations", h.ListCitationsByInstrument)
			r.Get("/legal/citations/{id}/categories", h.ListCategoriesByCitation)
			r.Get("/legal/categories/{id}/elements", h.ListElementsByCategory)
			r.Get("/legal/elements/{id}/subelements", h.ListSubElementsByElement)

			// URL<->offence linking — department-ownership-scoped like
			// /results and /domain, not global; see requireDomainOwnership.
			r.Get("/legal/offences/*", h.OffencesByURL)
			r.Post("/legal/offences/*", h.AttachOffence)
			r.Delete("/legal/offences/{id}", h.DetachOffence)

			// Cases <-> URL — department-ownership-scoped like the offence
			// routes above; AddCaseLetter is keyed by the case's own
			// surrogate ID, ownership resolved via the case's own
			// DepartmentID (db.Store.GetCase), not URL watchlist
			// membership — a case belongs to one department regardless of
			// how many other departments also watch its URL(s).
			r.Get("/cases/*", h.CasesByURL)
			r.Post("/cases/*", h.CreateCaseForURL)
			r.Patch("/cases/{id}", h.UpdateCase)
			r.Post("/cases/{id}/letters", h.AddCaseLetter)
			r.Post("/cases/{id}/urls", h.AddCaseURL)
			r.Patch("/cases/{id}/urls/{url_id}", h.UpdateCaseURLStatus)
			r.Patch("/cases/{id}/urls/{url_id}/agency", h.UpdateCaseURLAgency)
			r.Patch("/cases/{id}/letters/{letter_id}", h.UpdateCaseLetter)
			r.Delete("/cases/{id}/letters/{letter_id}", h.DeleteCaseLetter)

			// Docs page data source — every CaseLetter, admin: global,
			// non-admin: own department's cases only. See ListCaseLetters.
			r.Get("/case-letters", h.ListCaseLetters)
			r.Get("/case-letters/export", h.ExportCaseLetters)

			// Cases view's data source — see ListCaseSummaries.
			r.Get("/case-summaries", h.ListCaseSummaries)
			r.Get("/case-summaries/export", h.ExportCaseSummaries)

			// Reachable by a super admin OR a department admin — DNS servers
			// stay one shared/global catalog (no department scoping), while
			// user management is scoped to the caller's own department for
			// a department admin (enforced in the handlers).
			r.Group(func(r chi.Router) {
				r.Use(requireAnyAdmin)

				r.Post("/dns-servers", h.CreateDNSServer)
				r.Patch("/dns-servers/{id}", h.UpdateDNSServer)
				r.Delete("/dns-servers/{id}", h.DeleteDNSServer)
				r.Patch("/dns-servers/{id}/enabled", h.SetDNSServerEnabled)
				r.Post("/dns-servers/test", h.TestDNSServer)
				r.Post("/admin/isp-logos", h.UpsertISPLogo)
				r.Delete("/admin/isp-logos/*", h.DeleteISPLogo)
				r.Post("/admin/agencies", h.CreateAgency)
				r.Patch("/admin/agencies/{id}", h.UpdateAgency)
				r.Delete("/admin/agencies/{id}", h.DeleteAgency)
				r.Post("/admin/recipients", h.CreateRecipient)
				r.Patch("/admin/recipients/{id}", h.UpdateRecipient)
				r.Delete("/admin/recipients/{id}", h.DeleteRecipient)
				r.Post("/admin/requestors", h.CreateRequestor)
				r.Patch("/admin/requestors/{id}", h.UpdateRequestor)
				r.Delete("/admin/requestors/{id}", h.DeleteRequestor)
				r.Post("/due-date-presets", h.CreateDueDatePreset)
				r.Delete("/due-date-presets/{id}", h.DeleteDueDatePreset)

				r.Post("/legal/instruments", h.CreateInstrument)
				r.Patch("/legal/instruments/{id}", h.UpdateInstrument)
				r.Delete("/legal/instruments/{id}", h.DeleteInstrument)
				r.Post("/legal/citations/parse-preview", h.ParseCitationPreview)
				r.Post("/legal/citations", h.CreateCitation)
				r.Patch("/legal/citations/{id}", h.UpdateCitation)
				r.Delete("/legal/citations/{id}", h.DeleteCitation)
				r.Post("/legal/categories", h.CreateCategory)
				r.Patch("/legal/categories/{id}", h.UpdateCategory)
				r.Delete("/legal/categories/{id}", h.DeleteCategory)
				r.Post("/legal/elements", h.CreateElement)
				r.Patch("/legal/elements/{id}", h.UpdateElement)
				r.Delete("/legal/elements/{id}", h.DeleteElement)
				r.Post("/legal/subelements", h.CreateSubElement)
				r.Patch("/legal/subelements/{id}", h.UpdateSubElement)
				r.Delete("/legal/subelements/{id}", h.DeleteSubElement)

				r.Get("/admin/users", h.ListUsers)
				r.Post("/admin/users", h.CreateUser)
				r.Delete("/admin/users/{id}", h.DeleteUser)
				r.Post("/admin/users/{id}/reset-password", h.ResetUserPassword)
			})

			// Super-admin-only — inherently cross-department concerns.
			r.Group(func(r chi.Router) {
				r.Use(requireAdmin)

				r.Get("/admin/departments", h.ListDepartments)
				r.Post("/admin/departments", h.CreateDepartment)
				r.Patch("/admin/departments/{id}", h.UpdateDepartment)
				r.Patch("/admin/users/{id}", h.UpdateUser)
				r.Get("/admin/urls/unassigned", h.ListUnassignedURLs)
				r.Delete("/admin/urls/{id}", h.PurgeURL)

				r.Get("/admin/compliant-ips", h.ListCompliantIPs)
				r.Post("/admin/compliant-ips", h.CreateCompliantIP)
				r.Delete("/admin/compliant-ips/{id}", h.DeleteCompliantIP)

				r.Get("/admin/scan-interval", h.GetScanInterval)
				r.Patch("/admin/scan-interval", h.SetScanInterval)
			})
		})
	})
}
