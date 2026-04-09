package protocol

import "testing"

func TestRespValueCommandName(t *testing.T) {
	tests := []struct {
		name string
		value RespValue
		want string
	}{
		{
			name: "bulk string command lowercased",
			value: ArrayOf(BulkBytes([]byte("PING"))),
			want: "ping",
		},
		{
			name: "simple string command lowercased",
			value: ArrayOf(SimpleString("SET")),
			want: "set",
		},
		{
			name: "unicode simple string preserved",
			value: ArrayOf(SimpleString("ÄCMD")),
			want: "äcmd",
		},
		{
			name: "mixed ascii preserved and lowered",
			value: ArrayOf(BulkBytes([]byte("Config.GET"))),
			want: "config.get",
		},
		{
			name: "empty array returns empty",
			value: ArrayOf(),
			want: "",
		},
		{
			name: "non array returns empty",
			value: BulkBytes([]byte("PING")),
			want: "",
		},
		{
			name: "non string first arg returns empty",
			value: ArrayOf(Integer(1)),
			want: "",
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
