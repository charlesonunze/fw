package grpc

import (
	"context"
	"fmt"

	inventorypb "github.com/charlesonunze/fw/examples/ecomm/internal/modules/inventory/pb"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/inventory/service"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements the proto-generated InventoryServiceServer interface.
type Server struct {
	inventorypb.UnimplementedInventoryServiceServer
	svc *service.InventoryService
}

// New creates a new gRPC inventory server.
func New(svc *service.InventoryService) *Server {
	return &Server{svc: svc}
}

// Reserve handles the unary Reserve RPC.
func (s *Server) Reserve(ctx context.Context, req *inventorypb.ReserveRequest) (*inventorypb.ReserveResponse, error) {
	if len(req.GetItems()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no items to reserve")
	}

	items := make([]service.ReserveItem, len(req.GetItems()))
	for i, item := range req.GetItems() {
		items[i] = service.ReserveItem{
			ItemID:   item.GetItemId(),
			Quantity: item.GetQuantity(),
		}
	}

	reservationID, err := s.svc.Reserve(ctx, items)
	if err != nil {
		// Map errors to appropriate gRPC status codes
		return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("reservation failed: %v", err))
	}

	return &inventorypb.ReserveResponse{ReservationId: reservationID}, nil
}
