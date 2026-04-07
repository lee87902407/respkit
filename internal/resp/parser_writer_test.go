package resp

import (
	"bytes"
	"errors"
	"testing"
)

func TestParserParsesRESPTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType RespType
		wantArgs []string
		wantRaw  string
		wantPos  int
		complete bool
	}{
		{name: "array", input: "*2\r\n$4\r\nPING\r\n$4\r\npong\r\n", wantType: Array, wantArgs: []string{"PING", "pong"}, wantRaw: "*2\r\n$4\r\nPING\r\n$4\r\npong\r\n", wantPos: 24, complete: true},
		{name: "simple string", input: "+OK\r\n", wantType: SimpleString, wantArgs: []string{"OK"}, wantRaw: "+OK\r\n", wantPos: 5, complete: true},
		{name: "error", input: "-ERR boom\r\n", wantType: Error, wantArgs: []string{"ERR boom"}, wantRaw: "-ERR boom\r\n", wantPos: 11, complete: true},
		{name: "integer", input: ":42\r\n", wantType: Integer, wantArgs: []string{"42"}, wantRaw: ":42\r\n", wantPos: 5, complete: true},
		{name: "bulk string", input: "$5\r\nhello\r\n", wantType: BulkString, wantArgs: []string{"hello"}, wantRaw: "$5\r\nhello\r\n", wantPos: 11, complete: true},
		{name: "null bulk string", input: "$-1\r\n", wantType: BulkString, wantArgs: nil, wantRaw: "$-1\r\n", wantPos: 5, complete: true},
		{name: "inline command", input: "SET key value\r\n", wantType: Array, wantArgs: []string{"SET", "key", "value"}, wantRaw: "SET key value\r\n", wantPos: 15, complete: true},
		{name: "incomplete", input: "*2\r\n$4\r\nPING\r\n$", complete: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser([]byte(tt.input))
			result := parser.ParseNextCommand()
			if result.Complete != tt.complete {
				t.Fatalf("Complete = %v, want %v", result.Complete, tt.complete)
			}
			if !tt.complete {
				return
			}
			if result.Type != tt.wantType {
				t.Fatalf("Type = %q, want %q", result.Type, tt.wantType)
			}
			if string(result.Raw) != tt.wantRaw {
				t.Fatalf("Raw = %q, want %q", result.Raw, tt.wantRaw)
			}
			if parser.Position() != tt.wantPos {
				t.Fatalf("Position = %d, want %d", parser.Position(), tt.wantPos)
			}
			if len(result.Args) != len(tt.wantArgs) {
				t.Fatalf("len(Args) = %d, want %d", len(result.Args), len(tt.wantArgs))
			}
			for i, want := range tt.wantArgs {
				if got := string(result.Args[i]); got != want {
					t.Fatalf("Args[%d] = %q, want %q", i, got, want)
				}
			}
		})
	}
}

func TestParserResetAndPosition(t *testing.T) {
	parser := NewParser([]byte("+OK\r\n"))
	_ = parser.ParseNextCommand()
	if parser.Position() == 0 {
		t.Fatal("Position() should advance after parsing")
	}

	parser.Reset([]byte(":7\r\n"))
	if parser.Position() != 0 {
		t.Fatalf("Position after Reset = %d, want 0", parser.Position())
	}
	result := parser.ParseNextCommand()
	if !result.Complete || string(result.Args[0]) != "7" {
		t.Fatalf("unexpected parse result after Reset: %+v", result)
	}
}

func TestParserInlineArgsQuotesAndEscapes(t *testing.T) {
	parser := NewParser(nil)
	args := parser.parseInlineArgs([]byte("SET \"quoted value\" 'x\\ny' plain"))
	want := []string{"SET", "quoted value", "x\ny", "plain"}
	if len(args) != len(want) {
		t.Fatalf("len(args) = %d, want %d", len(args), len(want))
	}
	for i, got := range args {
		if string(got) != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, got, want[i])
		}
	}
	if parser.parseInlineArgs([]byte("SET \"unterminated")) != nil {
		t.Fatal("expected nil for unbalanced quotes")
	}
}

func TestParseIntAcceptsSignedValues(t *testing.T) {
	tests := []struct {
		input string
		want  int
		ok    bool
	}{
		{input: "0", want: 0, ok: true},
		{input: "7", want: 7, ok: true},
		{input: "+12", want: 12, ok: true},
		{input: "-9", want: -9, ok: true},
		{input: "12x", ok: false},
		{input: "", ok: false},
	}
	for _, tt := range tests {
		got, ok := parseInt([]byte(tt.input))
		if ok != tt.ok {
			t.Fatalf("parseInt(%q) ok = %v, want %v", tt.input, ok, tt.ok)
		}
		if ok && got != tt.want {
			t.Fatalf("parseInt(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestWriterEncodesRESPValues(t *testing.T) {
	w := NewWriter(64)
	w.AppendBulk([]byte("hello"))
	w.AppendArray(2)
	w.AppendError("ERR boom")
	w.AppendString("OK")
	w.AppendInt(9)
	w.AppendNull()

	want := "$5\r\nhello\r\n*2\r\n-ERR boom\r\n+OK\r\n:9\r\n$-1\r\n"
	if got := string(w.Bytes()); got != want {
		t.Fatalf("Bytes = %q, want %q", got, want)
	}
}

func TestWriterAppendAnyAndHelpers(t *testing.T) {
	w := NewWriter(128)
	w.AppendAny(nil)
	w.AppendAny(errors.New("nope"))
	w.AppendAny(AnySimpleError{Err: errors.New("raw")})
	w.AppendAny(AnySimpleString("OK"))
	w.AppendAny(AnySimpleInt(5))
	w.AppendAny(true)
	w.AppendAny(false)
	w.AppendAny([]byte("abc"))
	w.AppendAny([]byte(nil))
	w.AppendAny([]any{"x", int64(2)})
	w.AppendAny(map[string]any{"a": "b"})
	w.AppendAny(struct{ Name string }{Name: "demo"})

	got := string(w.Bytes())
	checks := []string{"$-1\r\n", "-ERR nope\r\n", "-raw\r\n", "+OK\r\n", ":5\r\n", "$1\r\n1\r\n", "$1\r\n0\r\n", "$3\r\nabc\r\n", "*2\r\n$1\r\nx\r\n$1\r\n2\r\n", "$6\r\n{demo}\r\n"}
	for _, check := range checks {
		if !bytes.Contains([]byte(got), []byte(check)) {
			t.Fatalf("encoded output missing %q in %q", check, got)
		}
	}
	if !bytes.Contains([]byte(got), []byte("$1\r\na\r\n$1\r\nb\r\n")) {
		t.Fatalf("map encoding missing key/value pair in %q", got)
	}
}

func TestWriterSanitizesAndFlushes(t *testing.T) {
	w := NewWriter(32)
	w.AppendString("hello\r\nworld")
	w.AppendError("bad\nnews")
	if got := string(w.Bytes()); got != "+hello  world\r\n-bad news\r\n" {
		t.Fatalf("sanitized output = %q", got)
	}
	if got := w.prefixERRIfNeeded("oops"); got != "ERR oops" {
		t.Fatalf("prefixERRIfNeeded lowercase = %q", got)
	}
	if got := w.prefixERRIfNeeded("WRONGTYPE boom"); got != "WRONGTYPE boom" {
		t.Fatalf("prefixERRIfNeeded uppercase = %q", got)
	}

	var buf bytes.Buffer
	if err := w.Flush(&buf); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("Flush() should write buffered data")
	}
	if len(w.Bytes()) != 0 {
		t.Fatalf("Bytes() length after Flush = %d, want 0", len(w.Bytes()))
	}

	w.AppendString("OK")
	w.Reset()
	if len(w.Bytes()) != 0 {
		t.Fatalf("Bytes() length after Reset = %d, want 0", len(w.Bytes()))
	}
}
