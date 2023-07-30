package main

import (
	"log"
	"net"

	"github.com/kvuchkov/ms-thesis-grpcp/example/api"
	"github.com/kvuchkov/ms-thesis-grpcp/example/backend/handler"
	"go.etcd.io/bbolt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	addr := ":8001"

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	db, err := bbolt.Open("./data.bin", 0600, bbolt.DefaultOptions)
	if err != nil {
		log.Fatalf("Cannot open db: %+v", err)
	}
	server := grpc.NewServer()
	reflection.Register(server)
	orderHandler := handler.Order{Db: db}
	api.RegisterOrderServiceServer(server, &orderHandler)
	log.Printf("Listening on %s...", addr)
	err = server.Serve(lis)
	if err != nil {
		log.Fatal(err)
	}
}
