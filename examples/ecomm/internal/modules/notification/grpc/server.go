package grpc

import (
	"context"

	notificationpb "github.com/charlesonunze/fw/examples/ecomm/internal/modules/notification/pb"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/notification/service"
)

// Server implements the proto-generated NotificationServiceServer interface.
type Server struct {
	notificationpb.UnimplementedNotificationServiceServer
	svc *service.NotificationService
}

// New creates a new gRPC notification server.
func New(svc *service.NotificationService) *Server {
	return &Server{svc: svc}
}

// Send handles the unary Send RPC.
func (s *Server) Send(ctx context.Context, req *notificationpb.SendRequest) (*notificationpb.SendResponse, error) {
	n, err := s.svc.Send(ctx, req.GetUserId(), req.GetMessage())
	if err != nil {
		return nil, err
	}
	return &notificationpb.SendResponse{NotificationId: n.ID}, nil
}

// Stream handles the server-side streaming Stream RPC.
// It subscribes to live notifications for the requested user and streams
// them as they arrive, until the client disconnects.
func (s *Server) Stream(req *notificationpb.StreamRequest, stream notificationpb.NotificationService_StreamServer) error {
	ch, cancel := s.svc.Subscribe(req.GetUserId())
	defer cancel()

	for {
		select {
		case n, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(&notificationpb.Notification{
				Id:         n.ID,
				UserId:     n.UserID,
				Message:    n.Message,
				SentAtUnix: n.SentAt.Unix(),
			}); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}
