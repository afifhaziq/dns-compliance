package server

import (
	"context"
	"log"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/storage"
	pb "github.com/afif/dns-tracking/proto"
)

type grpcServer struct {
	pb.UnimplementedComplianceServiceServer
	store       db.Store
	storage     storage.Storage
	broadcaster *Broadcaster
}

func NewGRPCServer(store db.Store, stor storage.Storage, broadcaster *Broadcaster) pb.ComplianceServiceServer {
	return &grpcServer{store: store, storage: stor, broadcaster: broadcaster}
}

func (s *grpcServer) Submit(ctx context.Context, report *pb.ComplianceReport) (*pb.Acknowledgement, error) {
	run, err := s.store.ActiveScanRun(ctx)
	if err != nil {
		log.Printf("grpc: fetch active scan run: %v", err)
	}
	var runID uint
	if run != nil {
		runID = run.ID
	}

	servers, _ := s.store.ListDNSServers(ctx)
	serverByName := make(map[string]uint, len(servers))
	for _, srv := range servers {
		serverByName[srv.Name] = srv.ID
	}

	urls, _ := s.store.ListURLs(ctx)
	urlIDByValue := make(map[string]uint, len(urls))
	for _, u := range urls {
		urlIDByValue[u.URL] = u.ID
	}

	for _, r := range report.Results {
		result := db.ScanResult{
			ScanRunID:   runID,
			URLID:       urlIDByValue[r.Url],
			URLValue:    r.Url,
			DNSServerID: serverByName[r.DnsServer],
			Compliant:   r.Compliant,
			ResolvedIP:  r.ResolvedIp,
			Error:       r.Error,
			ScannedAt:   time.Unix(r.Timestamp, 0),
		}

		if err := s.store.InsertResult(ctx, result); err != nil {
			log.Printf("grpc: insert result for %s: %v", r.Url, err)
			continue
		}

		if len(r.Screenshot) > 0 && s.storage != nil {
			url, err := s.storage.Upload(ctx, r.Screenshot)
			if err != nil {
				log.Printf("grpc: upload screenshot for %s: %v", r.Url, err)
				continue
			}
			// Find the just-inserted result to update its screenshot URL.
			// time.Time{} = no lower bound; we just need the row inserted above.
			results, err := s.store.ResultsByURL(ctx, r.Url, time.Time{}, time.Time{})
			if err == nil && len(results) > 0 {
				_ = s.store.UpdateScreenshot(ctx, results[0].ID, url)
			}
		}
	}

	if s.broadcaster != nil {
		if data, err := buildProgressPayload(ctx, s.store); err == nil && data != nil {
			s.broadcaster.Publish(data)
		}
	}

	return &pb.Acknowledgement{Ok: true}, nil
}
