package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"poc-ext-proc-plugin/extproc-server/pkg/extproc"
	extprocv3 "poc-ext-proc-plugin/extproc-server/pkg/proto/envoy/service/ext_proc/v3"
)

func main() {
	// Parse command line flags
	port := flag.Int("port", 9001, "gRPC server port")
	flag.Parse()

	// Override with environment variable if set
	if envPort := os.Getenv("GRPC_PORT"); envPort != "" {
		fmt.Sscanf(envPort, "%d", port)
	}

	log.Printf("Starting ext-proc gRPC server on port %d", *port)

	// Create listener
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	// Create gRPC server
	s := grpc.NewServer()

	// Register ext-proc service
	extProcServer := extproc.NewServer()
	extprocv3.RegisterExternalProcessorServer(s, extProcServer)

	// Register health service
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(s, healthServer)

	// Set serving status for health checks
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("envoy.service.ext_proc.v3.ExternalProcessor", grpc_health_v1.HealthCheckResponse_SERVING)

	// Enable server reflection for debugging
	reflection.Register(s)

	// Handle graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		log.Println("Shutting down gRPC server...")
		healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
		healthServer.SetServingStatus("envoy.service.ext_proc.v3.ExternalProcessor", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
		s.GracefulStop()
	}()

	log.Printf("ext-proc gRPC server listening on :%d", *port)
	log.Printf("Services registered: ExternalProcessor, Health, Reflection")

	// Start server
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
