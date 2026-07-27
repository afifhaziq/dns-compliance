package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/afif/dns-tracking/internal/dnsconfig"
	"github.com/afif/dns-tracking/internal/grpcauth"
	"github.com/afif/dns-tracking/internal/pipeline"
	pb "github.com/afif/dns-tracking/proto"
	"google.golang.org/grpc"
)

// controlServer implements CrawlerControl.StartSweep, reusing runSweep — the
// same function the standalone CLI path calls — against DNS servers and
// URLs carried in the request instead of parsed from local files.
type controlServer struct {
	pb.UnimplementedCrawlerControlServer
	conn          *grpc.ClientConn
	baseCfg       pipeline.Config
	waitIdle      time.Duration
	postIdleSleep time.Duration
	token         string

	mu      sync.Mutex
	running bool
}

func newControlServer(conn *grpc.ClientConn, baseCfg pipeline.Config, waitIdle, postIdleSleep time.Duration, token string) *controlServer {
	return &controlServer{conn: conn, baseCfg: baseCfg, waitIdle: waitIdle, postIdleSleep: postIdleSleep, token: token}
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

	runSweep(ctx, req.Urls, servers, cfg, s.waitIdle, s.postIdleSleep, s.conn, s.token, req.Screenshots)

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
func runListenMode(listenAddr string, tr grpcTransport, grpcAddr string, dnsWorkers, ssWorkers int, dnsTimeout, ssTimeout, waitIdle, postIdleSleep time.Duration) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var conn *grpc.ClientConn
	if grpcAddr != "" {
		var err error
		conn, err = grpc.NewClient(grpcAddr, tr.dialOpt)
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
	var opts []grpc.ServerOption
	if tr.creds != nil {
		opts = append(opts, grpc.Creds(tr.creds))
	}
	if ic := grpcauth.UnaryInterceptor(tr.token); ic != nil {
		opts = append(opts, grpc.UnaryInterceptor(ic))
	} else {
		log.Print("WARNING: StartSweep is unauthenticated — set --auth-token to require a shared secret")
	}
	grpcSrv := grpc.NewServer(opts...)
	pb.RegisterCrawlerControlServer(grpcSrv, newControlServer(conn, baseCfg, waitIdle, postIdleSleep, tr.token))

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
