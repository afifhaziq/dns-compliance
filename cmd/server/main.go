package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/dnsconfig"
	"github.com/afif/dns-tracking/internal/ipinfo"
	"github.com/afif/dns-tracking/internal/server"
	"github.com/afif/dns-tracking/internal/storage"
	"github.com/afif/dns-tracking/internal/whois"
	pb "github.com/afif/dns-tracking/proto"
	"github.com/go-chi/chi/v5"
	"google.golang.org/grpc"
	"gorm.io/driver/postgres"
)

func main() {
	httpAddr := flag.String("http-addr", ":8080", "HTTP listen address")
	grpcAddr := flag.String("grpc-addr", ":50051", "gRPC listen address")
	dbURL := flag.String("db-url", envOr("DB_URL", "host=localhost user=postgres password=postgres dbname=dns_compliance port=5432 sslmode=disable"), "PostgreSQL DSN")
	minioAddr := flag.String("minio-endpoint", envOr("MINIO_ENDPOINT", "localhost:9000"), "MinIO endpoint (host:port)")
	minioKey := flag.String("minio-access-key", envOr("MINIO_ACCESS_KEY", "minioadmin"), "MinIO access key")
	minioSecret := flag.String("minio-secret-key", envOr("MINIO_SECRET_KEY", "minioadmin"), "MinIO secret key")
	minioBucket := flag.String("minio-bucket", envOr("MINIO_BUCKET", "screenshots"), "MinIO bucket name")
	crawlerPath := flag.String("crawler-path", envOr("CRAWLER_PATH", "./crawler"), "path to crawler binary")
	seedFile := flag.String("seed-dns", "dns-server.yaml", "YAML file to seed DNS servers on first run; empty to skip")
	intervalMin := flag.Int("interval", 60, "scan interval in minutes")
	cookieSecure := flag.Bool("cookie-secure", envOr("COOKIE_SECURE", "true") == "true", "mark the session cookie Secure (disable for local plain-HTTP dev)")
	bootstrapAdminUser := flag.String("bootstrap-admin-username", envOr("BOOTSTRAP_ADMIN_USERNAME", ""), "username for the bootstrap admin, created only if the users table is empty")
	bootstrapAdminPass := flag.String("bootstrap-admin-password", envOr("BOOTSTRAP_ADMIN_PASSWORD", ""), "password for the bootstrap admin, created only if the users table is empty")
	ipinfoToken := flag.String("ipinfo-token", envOr("IPINFO_TOKEN", ""), "ipinfo.io API token for ASN/org lookups; empty uses the unauthenticated (lower rate limit) tier")
	whoisRefreshIntervalMin := flag.Int("whois-refresh-interval", 1440, "WHOIS/RDAP refresh sweep interval in minutes")
	whoisStaleDays := flag.Int("whois-stale-days", 30, "re-fetch a domain's WHOIS/RDAP data once its cached copy is older than this many days")
	flag.Parse()

	// Connect to PostgreSQL and run AutoMigrate.
	gormDB, err := db.Connect(postgres.Open(*dbURL))
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}

	// One-time idempotent backfill: canonicalize any pre-existing urls.url
	// rows and merge post-normalization duplicates, before anything else
	// reads from the urls table.
	if err := db.NormalizeAndDedupeURLs(context.Background(), gormDB); err != nil {
		log.Fatalf("normalize urls: %v", err)
	}

	if err := db.SeedDepartments(gormDB); err != nil {
		log.Printf("seed departments: %v", err)
	}

	if err := db.MigrateAdminDepartments(gormDB); err != nil {
		log.Fatalf("migrate admin departments: %v", err)
	}

	store := db.NewStore(gormDB)

	// Bootstrap admin — without this, a fresh deployment has no way to log
	// in at all. No-op once the users table is non-empty.
	if err := db.SeedAdmin(gormDB, *bootstrapAdminUser, *bootstrapAdminPass); err != nil {
		log.Printf("seed bootstrap admin: %v", err)
	}

	// Seed DNS servers from YAML if the table is empty.
	if *seedFile != "" {
		if cfg, err := dnsconfig.Load(*seedFile); err == nil {
			entries := make([]db.DNSServer, len(cfg.Servers))
			for i, s := range cfg.Servers {
				entries[i] = db.DNSServer{ISP: s.ISP, Name: s.Name, Address: s.Address, Protocol: s.Protocol}
			}
			if err := db.Seed(gormDB, entries); err != nil {
				log.Printf("seed DNS servers: %v", err)
			}
		} else {
			log.Printf("seed-dns: %v (skipping)", err)
		}
	}

	// MinIO client.
	stor, err := storage.NewMinioStorage(*minioAddr, *minioKey, *minioSecret, *minioBucket)
	if err != nil {
		log.Fatalf("minio: %v", err)
	}

	// ASN/org lookups via ipinfo.io, cached per-IP in the DB (see
	// internal/server/grpc.go's fetchAndCacheIPInfo) rather than called on
	// every scan.
	ipFetch := ipinfo.NewFetcher(*ipinfoToken)

	// Scanner manages crawler subprocess lifecycle.
	sc := server.NewScanner(*crawlerPath, *grpcAddr, store)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	broadcaster := server.NewBroadcaster()

	// gRPC server — receives scan results from the crawler.
	grpcLis, err := net.Listen("tcp", *grpcAddr)
	if err != nil {
		log.Fatalf("grpc listen: %v", err)
	}
	grpcSrv := grpc.NewServer()
	pb.RegisterComplianceServiceServer(grpcSrv, server.NewGRPCServer(store, stor, broadcaster, ipFetch, whois.FetchIP))
	go func() {
		log.Printf("gRPC listening on %s", *grpcAddr)
		if err := grpcSrv.Serve(grpcLis); err != nil {
			log.Printf("grpc serve: %v", err)
		}
	}()

	// Start the hourly scan scheduler.
	server.StartScheduler(ctx, sc, time.Duration(*intervalMin)*time.Minute)

	// Start the WHOIS/RDAP refresher — re-fetches stale DomainWhois rows on
	// a much slower cadence than the scan scheduler.
	server.StartWhoisRefresher(ctx, store, whois.Fetch, time.Duration(*whoisRefreshIntervalMin)*time.Minute, *whoisStaleDays)

	// HTTP server — REST API for the frontend.
	r := chi.NewRouter()
	server.RegisterRoutes(r, store, sc, broadcaster, *cookieSecure, whois.Fetch)

	httpSrv := &http.Server{Addr: *httpAddr, Handler: r}
	go func() {
		log.Printf("HTTP listening on %s", *httpAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("http serve: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	grpcSrv.GracefulStop()
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	httpSrv.Shutdown(shutCtx) //nolint:errcheck
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
