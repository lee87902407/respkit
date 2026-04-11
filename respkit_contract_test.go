package respkit_test

import (
	"testing"

	"github.com/lee87902407/basekit/mempool"
	"github.com/lee87902407/respkit"
	"github.com/lee87902407/respkit/internal/protocol"
)

func TestScopedRequestUsesRequestField(t *testing.T) {
	scope := &mempool.Scope{}
	value := protocol.SimpleString("PING")

	request := respkit.ScopedRequest{
		Request: value,
		Scope:   scope,
	}

	if request.Request.Type != value.Type {
		t.Fatalf("Request.Type = %v, want %v", request.Request.Type, value.Type)
	}
	if request.Scope != scope {
		t.Fatal("Scope was not retained on ScopedRequest")
	}
}

func TestScopedResponseUsesResponseField(t *testing.T) {
	scope := &mempool.Scope{}
	value := protocol.SimpleString("PONG")

	response := respkit.ScopedResponse{
		Response: value,
		Scope:    scope,
	}

	if response.Response.Type != value.Type {
		t.Fatalf("Response.Type = %v, want %v", response.Response.Type, value.Type)
	}
	if response.Scope != scope {
		t.Fatal("Scope was not retained on ScopedResponse")
	}
}
