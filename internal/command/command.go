package command

import "github.com/lee87902407/respkit/internal/protocol"

// BuildCommand constructs a basic executable command from a RESP request value.
func BuildCommand(request protocol.RespValue) Command {
	name := request.CommandName()
	args := request.CommandArgs()

	switch name {
	case "ping":
		return buildPingCommand(args)
	case "echo":
		return buildEchoCommand(args)
	default:
		return &UnknownCommand{name: name}
	}
}

func buildPingCommand(args [][]byte) Command {
	switch len(args) {
	case 0:
		return &PingCommand{}
	case 1:
		return &PingCommand{message: args[0], hasMessage: true}
	default:
		return &InvalidArgsCommand{name: "ping"}
	}
}

func buildEchoCommand(args [][]byte) Command {
	if len(args) != 1 {
		return &InvalidArgsCommand{name: "echo"}
	}
	return &EchoCommand{message: args[0]}
}
