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
	"github.com/afif/dns-tracking/internal/favicon"
	"github.com/afif/dns-tracking/internal/grpcauth"
	"github.com/afif/dns-tracking/internal/ipinfo"
	"github.com/afif/dns-tracking/internal/server"
	"github.com/afif/dns-tracking/internal/storage"
	"github.com/afif/dns-tracking/internal/subfinder"
	"github.com/afif/dns-tracking/internal/whois"
	pb "github.com/afif/dns-tracking/proto"
	"github.com/go-chi/chi/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
	crawlerAddr := flag.String("crawler-addr", envOr("CRAWLER_ADDR", "localhost:50052"), "gRPC address of the crawler's control service")
	crawlerToken := flag.String("crawler-token", envOr("CRAWLER_TOKEN", ""), "shared secret sent with StartSweep RPCs; must match the crawler's --auth-token")
	seedFile := flag.String("seed-dns", "dns-server.yaml", "YAML file to seed DNS servers on first run; empty to skip")
	intervalMin := flag.Int("interval", 60, "scan interval in minutes")
	cookieSecure := flag.Bool("cookie-secure", envOr("COOKIE_SECURE", "true") == "true", "mark the session cookie Secure (disable for local plain-HTTP dev)")
	bootstrapAdminUser := flag.String("bootstrap-admin-username", envOr("BOOTSTRAP_ADMIN_USERNAME", ""), "username for the bootstrap admin, created only if the users table is empty")
	bootstrapAdminPass := flag.String("bootstrap-admin-password", envOr("BOOTSTRAP_ADMIN_PASSWORD", ""), "password for the bootstrap admin, created only if the users table is empty")
	ipinfoToken := flag.String("ipinfo-token", envOr("IPINFO_TOKEN", ""), "ipinfo.io API token for ASN/org lookups; empty uses the unauthenticated (lower rate limit) tier")
	whoisRefreshIntervalMin := flag.Int("whois-refresh-interval", 1440, "WHOIS/RDAP refresh sweep interval in minutes")
	whoisStaleDays := flag.Int("whois-stale-days", 30, "re-fetch a domain's WHOIS/RDAP data once its cached copy is older than this many days")
	subfinderPath := flag.String("subfinder-path", envOr("SUBFINDER_PATH", "subfinder"), "path to subfinder binary; empty to disable subdomain enumeration")
	tlsCert := flag.String("tls-cert", envOr("TLS_CERT", ""), "PEM path to this binary's leaf certificate; enables mTLS when set together with --tls-key and --tls-ca")
	tlsKey := flag.String("tls-key", envOr("TLS_KEY", ""), "PEM path to the private key for --tls-cert")
	tlsCA := flag.String("tls-ca", envOr("TLS_CA", ""), "PEM path to the CA that signed both binaries' certificates")
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

	if err := db.SeedScanInterval(gormDB, *intervalMin); err != nil {
		log.Printf("seed scan interval: %v", err)
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	broadcaster := server.NewBroadcaster()

	// Scanner triggers sweeps on the crawler's persistent control service
	// over gRPC instead of exec'ing a local subprocess — see
	// docs/superpowers/specs/2026-07-22-split-crawler-dashboard-hosts-design.md.
	// gRPC security applies to both directions: dialing the crawler's control
	// service, and listening for the crawler's Submit calls.
	grpcCreds, tlsOn, err := grpcauth.Creds(*tlsCert, *tlsKey, *tlsCA)
	if err != nil {
		log.Fatalf("TLS config: %v", err)
	}
	dialOpt := grpc.WithTransportCredentials(insecure.NewCredentials())
	if tlsOn {
		dialOpt = grpc.WithTransportCredentials(grpcCreds)
	} else {
		log.Print("WARNING: gRPC links are unencrypted — set --tls-cert, --tls-key and --tls-ca to enable mTLS")
	}
	crawlerConn, err := grpc.NewClient(*crawlerAddr, dialOpt)
	if err != nil {
		log.Fatalf("connecting to crawler: %v", err)
	}
	defer crawlerConn.Close()
	sc := server.NewScanner(pb.NewCrawlerControlClient(crawlerConn), *crawlerToken, store, broadcaster)

	// gRPC server — receives scan results from the crawler.
	grpcLis, err := net.Listen("tcp", *grpcAddr)
	if err != nil {
		log.Fatalf("grpc listen: %v", err)
	}
	var grpcOpts []grpc.ServerOption
	if tlsOn {
		grpcOpts = append(grpcOpts, grpc.Creds(grpcCreds))
	}
	if ic := grpcauth.UnaryInterceptor(*crawlerToken); ic != nil {
		grpcOpts = append(grpcOpts, grpc.UnaryInterceptor(ic))
	} else {
		log.Print("WARNING: ComplianceService.Submit is unauthenticated — set --crawler-token to require a shared secret")
	}
	grpcSrv := grpc.NewServer(grpcOpts...)
	pb.RegisterComplianceServiceServer(grpcSrv, server.NewGRPCServer(store, stor, broadcaster, ipFetch, whois.FetchIP))
	go func() {
		log.Printf("gRPC listening on %s", *grpcAddr)
		if err := grpcSrv.Serve(grpcLis); err != nil {
			log.Printf("grpc serve: %v", err)
		}
	}()

	// Start the scan scheduler — cadence is admin-configurable via the
	// admin panel (db.ScanSettings); *intervalMin only seeds its initial
	// value and serves as a fallback if the setting can't be read.
	server.StartScheduler(ctx, sc, store, time.Duration(*intervalMin)*time.Minute)

	// Start the WHOIS/RDAP refresher — re-fetches stale DomainWhois rows on
	// a much slower cadence than the scan scheduler.
	server.StartWhoisRefresher(ctx, store, whois.Fetch, time.Duration(*whoisRefreshIntervalMin)*time.Minute, *whoisStaleDays)

	// HTTP server — REST API for the frontend.
	var subfinderFetch subfinder.Fetcher
	if *subfinderPath != "" {
		subfinderFetch = subfinder.NewFetcher(*subfinderPath)
	}
	r := chi.NewRouter()
	server.RegisterRoutes(r, store, sc, broadcaster, *cookieSecure, whois.Fetch, favicon.Fetch, subfinderFetch, ipFetch, whois.FetchIP)

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
