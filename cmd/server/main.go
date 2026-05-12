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
	"github.com/afif/dns-tracking/internal/server"
	"github.com/afif/dns-tracking/internal/storage"
	pb "github.com/afif/dns-tracking/proto"
	"github.com/go-chi/chi/v5"
	"google.golang.org/grpc"
	"gorm.io/driver/postgres"
)

func main() {
	httpAddr     := flag.String("http-addr", ":8080", "HTTP listen address")
	grpcAddr     := flag.String("grpc-addr", ":50051", "gRPC listen address")
	dbURL        := flag.String("db-url", envOr("DB_URL", "host=localhost user=postgres password=postgres dbname=dns_compliance port=5432 sslmode=disable"), "PostgreSQL DSN")
	minioAddr    := flag.String("minio-endpoint", envOr("MINIO_ENDPOINT", "localhost:9000"), "MinIO endpoint (host:port)")
	minioKey     := flag.String("minio-access-key", envOr("MINIO_ACCESS_KEY", "minioadmin"), "MinIO access key")
	minioSecret  := flag.String("minio-secret-key", envOr("MINIO_SECRET_KEY", "minioadmin"), "MinIO secret key")
	minioBucket  := flag.String("minio-bucket", envOr("MINIO_BUCKET", "screenshots"), "MinIO bucket name")
	crawlerPath  := flag.String("crawler-path", envOr("CRAWLER_PATH", "./crawler"), "path to crawler binary")
	seedFile     := flag.String("seed-dns", "dns-server.yaml", "YAML file to seed DNS servers on first run; empty to skip")
	intervalMin  := flag.Int("interval", 60, "scan interval in minutes")
	flag.Parse()

	// Connect to PostgreSQL and run AutoMigrate.
	gormDB, err := db.Connect(postgres.Open(*dbURL))
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	store := db.NewStore(gormDB)

	// Seed DNS servers from YAML if the table is empty.
	if *seedFile != "" {
		if cfg, err := dnsconfig.Load(*seedFile); err == nil {
			entries := make([]db.DNSServer, len(cfg.Servers))
			for i, s := range cfg.Servers {
				entries[i] = db.DNSServer{Name: s.Name, Address: s.Address, Protocol: s.Protocol}
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

	// Scanner manages crawler subprocess lifecycle.
	sc := server.NewScanner(*crawlerPath, *grpcAddr, store)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// gRPC server — receives scan results from the crawler.
	grpcLis, err := net.Listen("tcp", *grpcAddr)
	if err != nil {
		log.Fatalf("grpc listen: %v", err)
	}
	grpcSrv := grpc.NewServer()
	pb.RegisterComplianceServiceServer(grpcSrv, server.NewGRPCServer(store, stor))
	go func() {
		log.Printf("gRPC listening on %s", *grpcAddr)
		if err := grpcSrv.Serve(grpcLis); err != nil {
			log.Printf("grpc serve: %v", err)
		}
	}()

	// Start the hourly scan scheduler.
	server.StartScheduler(ctx, sc, time.Duration(*intervalMin)*time.Minute)

	// HTTP server — REST API for the frontend.
	r := chi.NewRouter()
	server.RegisterRoutes(r, store, sc)

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
