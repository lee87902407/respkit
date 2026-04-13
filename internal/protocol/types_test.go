package protocol

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lee87902407/basekit/mempool"
)

func TestRespValueCommandName(t *testing.T) {
	tests := []struct {
		name  string
		value RespValue
		want  string
	}{
		{
			name:  "bulk string command lowercased",
			value: ArrayOf(BulkBytes([]byte("PING"))),
			want:  "ping",
		},
		{
			name:  "simple string command lowercased",
			value: ArrayOf(SimpleString("SET")),
			want:  "set",
		},
		{
			name:  "unicode simple string preserved",
			value: ArrayOf(SimpleString("ÄCMD")),
			want:  "äcmd",
		},
		{
			name:  "mixed ascii preserved and lowered",
			value: ArrayOf(BulkBytes([]byte("Config.GET"))),
			want:  "config.get",
		},
		{
			name:  "empty array returns empty",
			value: ArrayOf(),
			want:  "",
		},
		{
			name:  "non array returns empty",
			value: BulkBytes([]byte("PING")),
			want:  "",
		},
		{
			name:  "non string first arg returns empty",
			value: ArrayOf(Integer(1)),
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.CommandName(); got != tt.want {
				t.Fatalf("CommandName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRespValueParseValue(t *testing.T) {
	value := ArrayOf(BulkFromString("PING"))
	buf := append(SerializeValue(value), []byte("tail")...)

	got, consumed, ok := ParseValue(buf)
	if !ok {
		t.Fatal("ParseValue() reported incomplete for complete payload")
	}
	if !got.Equal(value) {
		t.Fatalf("ParseValue() value = %#v, want %#v", got, value)
	}
	if consumed != len(SerializeValue(value)) {
		t.Fatalf("ParseValue() consumed = %d, want %d", consumed, len(SerializeValue(value)))
	}
}

func TestRespValueAppendSerialized(t *testing.T) {
	prefix := []byte("prefix")
	value := ArrayOf(BulkFromString("PING"))

	got := AppendSerialized(append([]byte(nil), prefix...), value)
	want := append(append([]byte(nil), prefix...), SerializeValue(value)...)
	if !bytes.Equal(got, want) {
		t.Fatalf("AppendSerialized() = %q, want %q", got, want)
	}
}

func TestReaderReadUsesScopeBuffers(t *testing.T) {
	reader := NewReader()
	pool := mempool.New(mempool.DefaultOptions())
	scope := mempool.NewScope(pool)
	defer scope.Close()

	value, err := reader.Read(strings.NewReader("*2\r\n$4\r\nECHO\r\n$5\r\nhello\r\n"), scope)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	want := ArrayOf(BulkFromString("ECHO"), BulkFromString("hello"))
	if !value.Equal(want) {
		t.Fatalf("Read() = %#v, want %#v", value, want)
	}
}

func BenchmarkRespValueCommandNameBulk(b *testing.B) {
	v := ArrayOf(BulkBytes([]byte("COMMAND.LIST")))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = v.CommandName()
	}
}

func BenchmarkRespValueCommandNameSimpleString(b *testing.B) {
	v := ArrayOf(SimpleString("COMMAND.LIST"))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = v.CommandName()
	}
}
