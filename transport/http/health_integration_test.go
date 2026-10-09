package fwhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charlesonunze/fw"
	fwgrpc "github.com/charlesonunze/fw/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
)

type healthProbeService struct {
	name  string
	check func(context.Context) error
}

func (s *healthProbeService) Name() string                     { return s.name }
func (s *healthProbeService) Health(ctx context.Context) error { return s.check(ctx) }
func (*healthProbeService) Close() error                       { return nil }

func TestReadinessIsolationAcrossTransports(t *testing.T) {
	httpTransport := New(newTestRouter(), Config{Addr: "127.0.0.1:0"})
	grpcTransport := fwgrpc.New(fwgrpc.Config{Addr: "127.0.0.1:0"})
	app := fw.New(fw.Config{
		Logger:     testLogger{},
		Transports: []fw.Transport{httpTransport, grpcTransport},
	})
	var slowCache, failedPostgres atomic.Bool
	cache := &healthProbeService{name: "cache", check: func(ctx context.Context) error {
		if slowCache.Load() {
			<-ctx.Done()
		}
		return ctx.Err()
	}}
	postgres := &healthProbeService{name: "postgres", check: func(ctx context.Context) error {
		if failedPostgres.Load() {
			return errors.New("postgres unavailable")
		}
		return ctx.Err()
	}}
	if err := app.RegisterService(cache, fw.OptionalReadiness()); err != nil {
		t.Fatal(err)
	}
	if err := app.RegisterService(postgres); err != nil {
		t.Fatal(err)
	}
	appCtx, cancelApp := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- app.Start(appCtx) }()
	t.Cleanup(func() {
		cancelApp()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Start() error = %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("application did not stop")
		}
	})

	client := &http.Client{Timeout: 3 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	startupDeadline := time.Now().Add(5 * time.Second)
	for {
		if time.Now().After(startupDeadline) {
			t.Fatal("application did not become ready")
		}
		if addr := httpTransport.Addr(); addr != nil {
			response, err := client.Get("http://" + addr.String() + "/health/ready")
			if err == nil {
				response.Body.Close()
				if response.StatusCode == http.StatusOK {
					break
				}
			}
		}
		time.Sleep(time.Millisecond)
	}
	conn, err := grpc.NewClient(
		"passthrough:///"+grpcTransport.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	grpcClient := grpc_health_v1.NewHealthClient(conn)

	for _, tt := range []struct {
		name           string
		slowCache      bool
		failedPostgres bool
		httpStatus     int
		grpcStatus     grpc_health_v1.HealthCheckResponse_ServingStatus
	}{
		{"optional timeout", true, false, http.StatusOK, grpc_health_v1.HealthCheckResponse_SERVING},
		{"required failure", true, true, http.StatusServiceUnavailable, grpc_health_v1.HealthCheckResponse_NOT_SERVING},
		{"recovered", false, false, http.StatusOK, grpc_health_v1.HealthCheckResponse_SERVING},
	} {
		t.Run(tt.name, func(t *testing.T) {
			slowCache.Store(tt.slowCache)
			failedPostgres.Store(tt.failedPostgres)
			response, err := client.Get("http://" + httpTransport.Addr().String() + "/health/ready")
			if err != nil {
				t.Fatalf("readiness request: %v", err)
			}
			defer response.Body.Close()
			if response.StatusCode != tt.httpStatus {
				t.Fatalf("HTTP status = %d, want %d", response.StatusCode, tt.httpStatus)
			}
			var body readinessResponse
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			wantCache, wantPostgres := "ok", "ok"
			if tt.slowCache {
				wantCache = "error"
			}
			if tt.failedPostgres {
				wantPostgres = "error"
			}
			if body.Services["cache"].Status != wantCache || body.Services["postgres"].Status != wantPostgres {
				t.Fatalf("HTTP health services = %+v", body.Services)
			}

			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			result, err := grpcClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
			if err != nil {
				t.Fatalf("gRPC health request: %v", err)
			}
			if result.GetStatus() != tt.grpcStatus {
				t.Fatalf("gRPC status = %v, want %v", result.GetStatus(), tt.grpcStatus)
			}
		})
	}
}
