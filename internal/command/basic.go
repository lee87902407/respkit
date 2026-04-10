package command

import "github.com/lee87902407/respkit/internal/protocol"

// PingCommand implements the RESP PING command.
type PingCommand struct {
	message    []byte
	hasMessage bool
}

// Execute returns PONG or the provided message.
func (c *PingCommand) Execute(ctx *Context) protocol.RespValue {
	if !c.hasMessage {
		return protocol.SimpleString("PONG")
	}
	return protocol.BulkBytes(c.message)
}

// EchoCommand implements the RESP ECHO command.
type EchoCommand struct {
	message []byte
}

// Execute echoes the provided message.
func (c *EchoCommand) Execute(ctx *Context) protocol.RespValue {
	return protocol.BulkBytes(c.message)
}

// InvalidArgsCommand returns a standard wrong-number-of-arguments error.
type InvalidArgsCommand struct {
	name string
}

// Execute returns the invalid-arity error response.
func (c *InvalidArgsCommand) Execute(ctx *Context) protocol.RespValue {
	return protocol.Error("ERR wrong number of arguments for '" + c.name + "' command")
}

// UnknownCommand returns the standard Redis unknown-command error.
type UnknownCommand struct {
	name string
}

// Execute returns the unknown command error response.
func (c *UnknownCommand) Execute(ctx *Context) protocol.RespValue {
	return NotFoundError(c.name)
}
