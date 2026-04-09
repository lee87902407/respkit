package command

import "net"

// Context holds the execution context for a command.
type Context struct {
	// Command name (lowercase).
	Name string

	// Raw arguments (zero-copy into read buffer).
	Args [][]byte

	// Network connection for direct I/O (e.g., PubSub).
	RemoteAddr net.Addr

	// SessionID identifies the connection.
	SessionID uint64
}
