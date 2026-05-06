package sender

import (
	"context"

	pb "github.com/afif/dns-tracking/proto"
	"google.golang.org/grpc"
)

// Send submits a ComplianceReport to the gRPC server over conn.
func Send(ctx context.Context, conn *grpc.ClientConn, report *pb.ComplianceReport) error {
	client := pb.NewComplianceServiceClient(conn)
	_, err := client.Submit(ctx, report)
	return err
}
