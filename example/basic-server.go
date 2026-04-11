package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/lee87902407/respkit"
)

func main() {
	server := respkit.NewServer(&respkit.Config{
		Addr:    ":6380",
		Network: "tcp",
	})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		if err := server.Shutdown(); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	}()

	log.Println("Starting server on :6380")
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
	log.Println("Server stopped")
}
