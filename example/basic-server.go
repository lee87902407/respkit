package main

import (
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/lee87902407/respkit"
)

func newExampleMux() *respkit.Mux {
	var (
		mu    sync.RWMutex
		store = make(map[string][]byte)
	)

	mux := respkit.NewMux()
	mux.HandleFunc("ping", func(ctx *respkit.Context) error {
		return ctx.Conn.WriteString("PONG")
	})
	mux.HandleFunc("set", func(ctx *respkit.Context) error {
		if len(ctx.Command.Args) != 3 {
			return ctx.Conn.WriteError("ERR wrong number of arguments for 'set'")
		}
		mu.Lock()
		store[string(ctx.Command.Args[1])] = append([]byte(nil), ctx.Command.Args[2]...)
		mu.Unlock()
		return ctx.Conn.WriteString("OK")
	})
	mux.HandleFunc("get", func(ctx *respkit.Context) error {
		if len(ctx.Command.Args) != 2 {
			return ctx.Conn.WriteError("ERR wrong number of arguments for 'get'")
		}
		mu.RLock()
		value, ok := store[string(ctx.Command.Args[1])]
		mu.RUnlock()
		if !ok {
			return ctx.Conn.WriteNull()
		}
		return ctx.Conn.WriteBulk(value)
	})

	return mux
}

func main() {
	server := respkit.NewServer(&respkit.Config{
		Addr:    ":6380",
		Network: "tcp",
	}, newExampleMux())

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
