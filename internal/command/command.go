package command

import "github.com/lee87902407/respkit/internal/protocol"

// BuildCommand constructs a basic executable command from a RESP request value.
func BuildCommand(request protocol.RespValue) Command {
	name := request.CommandName()
	args := request.CommandArgs()

	switch name {
	case "ping":
		return &PingCommand{message: pingMessage(args)}
	case "echo":
		return &EchoCommand{message: firstArg(args)}
	default:
		return &UnknownCommand{name: name}
	}
}

func firstArg(args [][]byte) []byte {
	if len(args) == 0 {
		return nil
	}
	return args[0]
}

func pingMessage(args [][]byte) []byte {
	return firstArg(args)
}
