package sender

import (
	"context"

	"github.com/afif/dns-tracking/internal/grpcauth"
	pb "github.com/afif/dns-tracking/proto"
	"google.golang.org/grpc"
)

// Send submits a ComplianceReport to the gRPC server over conn, presenting
// the shared secret. An empty token sends no credentials at all, matching the
// server's behaviour when it has none configured.
func Send(ctx context.Context, conn *grpc.ClientConn, token string, report *pb.ComplianceReport) error {
	if token != "" {
		ctx = grpcauth.AppendToken(ctx, token)
	}
	client := pb.NewComplianceServiceClient(conn)
	_, err := client.Submit(ctx, report)
	return err
}
