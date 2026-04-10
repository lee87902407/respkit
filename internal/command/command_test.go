package command

import (
	"testing"

	"github.com/lee87902407/respkit/internal/protocol"
)

func TestBuildCommand(t *testing.T) {
	t.Run("returns ping command for ping request", func(t *testing.T) {
		request := protocol.ArrayOf(protocol.BulkFromString("PING"))

		cmd := BuildCommand(request)

		if _, ok := cmd.(*PingCommand); !ok {
			t.Fatalf("BuildCommand() returned %T, want *PingCommand", cmd)
		}
	})

	t.Run("returns echo command for echo request", func(t *testing.T) {
		request := protocol.ArrayOf(
			protocol.BulkFromString("ECHO"),
			protocol.BulkFromString("hello"),
		)

		cmd := BuildCommand(request)

		if _, ok := cmd.(*EchoCommand); !ok {
			t.Fatalf("BuildCommand() returned %T, want *EchoCommand", cmd)
		}
	})

	t.Run("ping with too many args returns invalid args error", func(t *testing.T) {
		request := protocol.ArrayOf(
			protocol.BulkFromString("PING"),
			protocol.BulkFromString("one"),
			protocol.BulkFromString("two"),
		)

		got := BuildCommand(request).Execute(&Context{})
		want := protocol.Error("ERR wrong number of arguments for 'ping' command")
		if !got.Equal(want) {
			t.Fatalf("Execute() = %#v, want %#v", got, want)
		}
	})

	t.Run("echo with no args returns invalid args error", func(t *testing.T) {
		request := protocol.ArrayOf(protocol.BulkFromString("ECHO"))

		got := BuildCommand(request).Execute(&Context{})
		want := protocol.Error("ERR wrong number of arguments for 'echo' command")
		if !got.Equal(want) {
			t.Fatalf("Execute() = %#v, want %#v", got, want)
		}
	})

	t.Run("echo with extra args returns invalid args error", func(t *testing.T) {
		request := protocol.ArrayOf(
			protocol.BulkFromString("ECHO"),
			protocol.BulkFromString("one"),
			protocol.BulkFromString("two"),
		)

		got := BuildCommand(request).Execute(&Context{})
		want := protocol.Error("ERR wrong number of arguments for 'echo' command")
		if !got.Equal(want) {
			t.Fatalf("Execute() = %#v, want %#v", got, want)
		}
	})

	t.Run("returns unknown command error response", func(t *testing.T) {
		request := protocol.ArrayOf(protocol.BulkFromString("NOPE"))

		cmd := BuildCommand(request)
		got := cmd.Execute(&Context{})
		want := protocol.Error("ERR unknown command 'nope'")
		if !got.Equal(want) {
			t.Fatalf("Execute() = %#v, want %#v", got, want)
		}
	})
}

func TestBasicCommandExecute(t *testing.T) {
	t.Run("ping without args returns PONG", func(t *testing.T) {
		cmd := &PingCommand{}

		got := cmd.Execute(&Context{})
		want := protocol.SimpleString("PONG")
		if !got.Equal(want) {
			t.Fatalf("Execute() = %#v, want %#v", got, want)
		}
	})

	t.Run("ping with message returns bulk string", func(t *testing.T) {
		cmd := &PingCommand{message: []byte("hello"), hasMessage: true}

		got := cmd.Execute(&Context{})
		want := protocol.BulkBytes([]byte("hello"))
		if !got.Equal(want) {
			t.Fatalf("Execute() = %#v, want %#v", got, want)
		}
	})

	t.Run("ping with empty bulk string returns empty bulk string", func(t *testing.T) {
		cmd := &PingCommand{message: []byte{}, hasMessage: true}

		got := cmd.Execute(&Context{})
		want := protocol.BulkBytes([]byte{})
		if !got.Equal(want) {
			t.Fatalf("Execute() = %#v, want %#v", got, want)
		}
	})

	t.Run("echo returns echoed bulk string", func(t *testing.T) {
		cmd := &EchoCommand{message: []byte("world")}

		got := cmd.Execute(&Context{})
		want := protocol.BulkBytes([]byte("world"))
		if !got.Equal(want) {
			t.Fatalf("Execute() = %#v, want %#v", got, want)
		}
	})

	t.Run("invalid args command returns wrong-number error", func(t *testing.T) {
		cmd := &InvalidArgsCommand{name: "echo"}

		got := cmd.Execute(&Context{})
		want := protocol.Error("ERR wrong number of arguments for 'echo' command")
		if !got.Equal(want) {
			t.Fatalf("Execute() = %#v, want %#v", got, want)
		}
	})
}
