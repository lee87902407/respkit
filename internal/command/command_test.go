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
