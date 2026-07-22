package main

import (
	"context"
	"crypto/subtle"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/afif/dns-tracking/internal/dnsconfig"
	"github.com/afif/dns-tracking/internal/pipeline"
	pb "github.com/afif/dns-tracking/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const authMetadataKey = "x-auth-token"

// authInterceptor rejects any RPC whose x-auth-token metadata doesn't match
// token, using a constant-time comparison — this is a real credential
// check, not a lookup, so timing must not leak partial matches.
func authInterceptor(token string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing auth token")
		}
		got := strings.Join(md.Get(authMetadataKey), "")
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			return nil, status.Error(codes.Unauthenticated, "invalid auth token")
		}
		return handler(ctx, req)
	}
}

// controlServer implements CrawlerControl.StartSweep, reusing runSweep — the
// same function the standalone CLI path calls — against DNS servers and
// URLs carried in the request instead of parsed from local files.
type controlServer struct {
	pb.UnimplementedCrawlerControlServer
	conn          *grpc.ClientConn
	baseCfg       pipeline.Config
	waitIdle      time.Duration
	postIdleSleep time.Duration

	mu      sync.Mutex
	running bool
}

func newControlServer(conn *grpc.ClientConn, baseCfg pipeline.Config, waitIdle, postIdleSleep time.Duration) *controlServer {
	return &controlServer{conn: conn, baseCfg: baseCfg, waitIdle: waitIdle, postIdleSleep: postIdleSleep}
}

func (s *controlServer) StartSweep(ctx context.Context, req *pb.SweepRequest) (*pb.SweepAck, error) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return &pb.SweepAck{Accepted: false, Error: "sweep already in progress"}, nil
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	servers := buildServerEntries(dnsServerConfigsToServers(req.DnsServers))

	cfg := s.baseCfg
	cfg.CompliantIPs = req.CompliantIps

	runSweep(ctx, req.Urls, servers, cfg, s.waitIdle, s.postIdleSleep, s.conn, req.Screenshots)

	return &pb.SweepAck{Accepted: true}, nil
}

func dnsServerConfigsToServers(configs []*pb.DNSServerConfig) []dnsconfig.Server {
	servers := make([]dnsconfig.Server, len(configs))
	for i, c := range configs {
		servers[i] = dnsconfig.Server{ISP: c.Isp, Name: c.Name, Address: c.Address, Protocol: c.Protocol}
	}
	return servers
}

// runListenMode starts a persistent gRPC server exposing CrawlerControl,
// used by the dashboard to trigger sweeps remotely instead of exec'ing this
// binary as a local subprocess — see
// docs/superpowers/specs/2026-07-22-split-crawler-dashboard-hosts-design.md.
func runListenMode(listenAddr, authToken, grpcAddr string, dnsWorkers, ssWorkers int, dnsTimeout, ssTimeout, waitIdle, postIdleSleep time.Duration) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var conn *grpc.ClientConn
	if grpcAddr != "" {
		var err error
		conn, err = grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Fatalf("connecting to gRPC server: %v", err)
		}
		defer conn.Close()
	}

	baseCfg := pipeline.Config{
		DNSWorkers:        dnsWorkers,
		ScreenshotWorkers: ssWorkers,
		DNSTimeout:        dnsTimeout,
		ScreenshotTimeout: ssTimeout,
	}

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	grpcSrv := grpc.NewServer(grpc.UnaryInterceptor(authInterceptor(authToken)))
	pb.RegisterCrawlerControlServer(grpcSrv, newControlServer(conn, baseCfg, waitIdle, postIdleSleep))

	go func() {
		<-ctx.Done()
		log.Println("shutting down")
		grpcSrv.GracefulStop()
	}()

	log.Printf("crawler control listening on %s", listenAddr)
	if err := grpcSrv.Serve(lis); err != nil {
		log.Printf("serve: %v", err)
	}
}
