package main

import (
	"log"
	"sync"

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

func newExampleServer(addr string) *respkit.Server {
	return respkit.NewServer(&respkit.Config{Addr: addr, Network: "tcp"}, newExampleMux())
}

func main() {
	server := newExampleServer(":6380")
	log.Println("respkit example listening on :6380")
	log.Fatal(server.ListenAndServe())
}
