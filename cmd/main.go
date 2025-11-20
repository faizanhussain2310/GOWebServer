package main

import (
	"log"
	"webserver/internal/protocol"
	"webserver/internal/server"
)

func main() {
	addr := "127.0.0.1:8080"

	config := protocol.NewHTTP11Config()

	srv := server.NewServerWithVersion(addr, config)

	log.Printf("Starting server on %s with %s", addr, config.Version)
	if err := srv.Start(); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}