package command

import (
	"fmt"
	"strings"
	"sync"

	"github.com/lee87902407/respkit/internal/protocol"
)

// Command is the interface for executable business commands.
type Command interface {
	Execute(ctx *Context) protocol.RespValue
}

// CommandFactory creates a Command from a name and raw arguments.
type CommandFactory func(name string, args [][]byte) (Command, error)

// Registry maintains the mapping from command names to factories.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]CommandFactory
	notFound  CommandFactory
}

// NewRegistry creates a new command registry.
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]CommandFactory),
	}
}

// Register adds a command factory for the given name.
func (r *Registry) Register(name string, factory CommandFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[strings.ToLower(name)] = factory
}

// SetNotFound sets the factory used for unknown commands.
func (r *Registry) SetNotFound(factory CommandFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notFound = factory
}

// Lookup finds the factory for a command name.
// Returns nil if not found and no NotFound handler is set.
func (r *Registry) Lookup(name string) CommandFactory {
	key := strings.ToLower(name)
	r.mu.RLock()
	factory, ok := r.factories[key]
	r.mu.RUnlock()
	if ok {
		return factory
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.notFound
}

// HandleFunc is a convenience type for simple command handlers.
type HandleFunc func(ctx *Context) protocol.RespValue

// Command adapter for HandleFunc.
type funcCommand struct {
	fn   HandleFunc
	ctx  *Context
}

func (c *funcCommand) Execute(ctx *Context) protocol.RespValue {
	return c.fn(ctx)
}

// FuncFactory creates a CommandFactory from a HandleFunc.
func FuncFactory(fn HandleFunc) CommandFactory {
	return func(name string, args [][]byte) (Command, error) {
		return &funcCommand{fn: fn}, nil
	}
}

// NotFoundError creates a standard Redis unknown-command error.
func NotFoundError(cmdName string) protocol.RespValue {
	return protocol.Error(fmt.Sprintf("ERR unknown command '%s'", cmdName))
}
