package respkit

import (
	"strings"

	"github.com/lee87902407/respkit/internal/protocol"
)

type muxHandler func(*Context) error

// Mux routes commands to registered handlers.
type Mux struct {
	handlers map[string]muxHandler
	notFound muxHandler
}

// NewMux creates a new command multiplexer.
func NewMux() *Mux {
	return &Mux{
		handlers: make(map[string]muxHandler),
		notFound: func(ctx *Context) error {
			normalized := protocol.NormalizeCommandNameBytes(ctx.Command.Args[0])
			return ctx.Conn.WriteError("ERR unknown command '" + normalized + "'")
		},
	}
}

// Register adds a handler for a command name.
func (m *Mux) Register(cmd string, handler func(*Context) error) {
	if handler == nil {
		panic("respkit: nil handler")
	}
	m.handlers[strings.ToLower(cmd)] = handler
}

// HandleFunc registers a function handler for a command name.
func (m *Mux) HandleFunc(cmd string, handler func(*Context) error) {
	m.Register(cmd, handler)
}

// SetNotFound sets the fallback handler used when no command matches.
func (m *Mux) SetNotFound(handler func(*Context) error) {
	if handler == nil {
		return
	}
	m.notFound = handler
}

// HandleCommand dispatches a command to the registered handler.
func (m *Mux) HandleCommand(ctx *Context) error {
	if len(ctx.Command.Args) == 0 {
		return nil
	}
	cmd := protocol.NormalizeCommandNameBytes(ctx.Command.Args[0])
	if handler, ok := m.handlers[cmd]; ok {
		return handler(ctx)
	}
	return m.notFound(ctx)
}

func (m *Mux) Handle(ctx *Context) error {
	return m.HandleCommand(ctx)
}
