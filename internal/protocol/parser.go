package protocol

import (
	"errors"
	"fmt"
	"io"

	"github.com/lee87902407/basekit/mempool"
)

var (
	errIncompleteCommand      = errors.New("incomplete command")
	errInvalidBulkLength      = errors.New("invalid bulk length")
	errInvalidMultiBulkLength = errors.New("invalid multibulk length")
	errProtocol               = errors.New("protocol error")
)

const defaultReadBufferSize = 4096

// Reader incrementally reads RESP values from an io.Reader.
type Reader struct {
	parser *Parser
	buf    []byte
}

// NewReader creates a Reader with an empty parse buffer.
func NewReader() *Reader {
	return &Reader{parser: NewParser()}
}

// Read reads and parses the next RESP value into the provided scope.
func (r *Reader) Read(src io.Reader, scope *mempool.Scope) (RespValue, error) {
	if scope == nil {
		return RespValue{}, fmt.Errorf("protocol: read scope is nil")
	}

	for {
		value, consumed, ok := r.parser.ParseOne(r.buf)
		if ok {
			value = copyValueIntoScope(scope, value)
			r.compact(consumed)
			return value, nil
		}

		chunk := scope.Get(defaultReadBufferSize)
		n, err := src.Read(chunk)
		if n > 0 {
			r.buf = append(r.buf, chunk[:n]...)
		}
		if err != nil {
			return RespValue{}, err
		}
	}
}

func (r *Reader) compact(consumed int) {
	if consumed >= len(r.buf) {
		r.buf = r.buf[:0]
		return
	}
	copy(r.buf, r.buf[consumed:])
	r.buf = r.buf[:len(r.buf)-consumed]
}

func copyValueIntoScope(scope *mempool.Scope, value RespValue) RespValue {
	switch value.Type {
	case TypeBulkString:
		if value.Bulk == nil {
			return value
		}
		dup := scope.Get(len(value.Bulk))
		copy(dup, value.Bulk)
		value.Bulk = dup[:len(value.Bulk)]
		return value
	case TypeArray:
		if len(value.Array) == 0 {
			return value
		}
		arr := make([]RespValue, len(value.Array))
		for i := range value.Array {
			arr[i] = copyValueIntoScope(scope, value.Array[i])
		}
		value.Array = arr
		return value
	default:
		return value
	}
}

// Parser implements zero-copy RESP parsing.
// Returned RespValue elements may reference the input buffer;
// the caller must ensure the buffer remains valid during use.
type Parser struct {
	buf   []byte
	pos   int
	marks []int
}

// NewParser creates a new zero-copy RESP parser.
func NewParser() *Parser {
	return &Parser{
		marks: make([]int, 0, 16),
	}
}

// Parse parses all complete RespValue elements from buf.
// Returns the parsed values and the number of bytes consumed.
// Returned values may reference buf (zero-copy).
func (p *Parser) Parse(buf []byte) ([]RespValue, int) {
	p.buf = buf
	p.pos = 0
	p.marks = p.marks[:0]

	var values []RespValue
	for p.pos < len(p.buf) {
		v, ok := p.parseNext()
		if !ok {
			break
		}
		values = append(values, v)
	}
	return values, p.pos
}

// ParseOne parses the first complete RESP value from buf.
func (p *Parser) ParseOne(buf []byte) (RespValue, int, bool) {
	p.buf = buf
	p.pos = 0
	p.marks = p.marks[:0]

	value, ok := p.parseNext()
	if !ok {
		return RespValue{}, 0, false
	}
	return value, p.pos, true
}

// parseNext parses one RESP value. Returns false if incomplete.
func (p *Parser) parseNext() (RespValue, bool) {
	if p.pos >= len(p.buf) {
		return RespValue{}, false
	}

	switch p.buf[p.pos] {
	case '*':
		return p.parseArray()
	case '+':
		return p.parseSimpleString()
	case '-':
		return p.parseError()
	case ':':
		return p.parseInt()
	case '$':
		return p.parseBulkString()
	default:
		return p.parseInlineCommand()
	}
}

func (p *Parser) parseArray() (RespValue, bool) {
	start := p.pos
	p.marks = p.marks[:0]

	lineEnd := p.findCRLF(p.pos + 1)
	if lineEnd == -1 {
		return RespValue{}, false
	}

	count, ok := parseIntBuf(p.buf[p.pos+1 : lineEnd-1])
	if !ok || count < 0 {
		return RespValue{}, false
	}

	// Null array: *-1\r\n
	if count == -1 {
		p.pos = lineEnd + 1
		return Null(), true
	}

	p.pos = lineEnd + 1

	elements := make([]RespValue, 0, count)
	for i := 0; i < count; i++ {
		elem, ok := p.parseNext()
		if !ok {
			p.pos = start
			return RespValue{}, false
		}
		elements = append(elements, elem)
	}

	_ = start
	return RespValue{Type: TypeArray, Array: elements}, true
}

func (p *Parser) parseSimpleString() (RespValue, bool) {
	lineEnd := p.findCRLF(p.pos + 1)
	if lineEnd == -1 {
		return RespValue{}, false
	}

	s := string(p.buf[p.pos+1 : lineEnd-1])
	p.pos = lineEnd + 1
	return SimpleString(s), true
}

func (p *Parser) parseError() (RespValue, bool) {
	lineEnd := p.findCRLF(p.pos + 1)
	if lineEnd == -1 {
		return RespValue{}, false
	}

	s := string(p.buf[p.pos+1 : lineEnd-1])
	p.pos = lineEnd + 1
	return Error(s), true
}

func (p *Parser) parseInt() (RespValue, bool) {
	lineEnd := p.findCRLF(p.pos + 1)
	if lineEnd == -1 {
		return RespValue{}, false
	}

	n, ok := parseInt64(p.buf[p.pos+1 : lineEnd-1])
	if !ok {
		return RespValue{}, false
	}

	p.pos = lineEnd + 1
	return Integer(n), true
}

func (p *Parser) parseBulkString() (RespValue, bool) {
	lineEnd := p.findCRLF(p.pos + 1)
	if lineEnd == -1 {
		return RespValue{}, false
	}

	bulkLen, ok := parseInt64(p.buf[p.pos+1 : lineEnd-1])
	if !ok {
		return RespValue{}, false
	}

	// Null bulk: $-1\r\n
	if bulkLen == -1 {
		p.pos = lineEnd + 1
		return Null(), true
	}

	if bulkLen < 0 {
		return RespValue{}, false
	}

	dataStart := lineEnd + 1
	dataEnd := dataStart + int(bulkLen)

	if dataEnd+2 > len(p.buf) {
		return RespValue{}, false
	}

	if p.buf[dataEnd] != '\r' || p.buf[dataEnd+1] != '\n' {
		return RespValue{}, false
	}

	p.pos = dataEnd + 2
	return RespValue{Type: TypeBulkString, Bulk: p.buf[dataStart:dataEnd]}, true
}

func (p *Parser) parseInlineCommand() (RespValue, bool) {
	newlinePos := p.findCRLF(p.pos)
	if newlinePos == -1 {
		return RespValue{}, false
	}

	var line []byte
	if newlinePos > p.pos && p.buf[newlinePos-1] == '\r' {
		line = p.buf[p.pos : newlinePos-1]
	} else {
		line = p.buf[p.pos:newlinePos]
	}

	args := p.parseInlineArgs(line)
	p.pos = newlinePos + 1

	if len(args) == 0 {
		return RespValue{Type: TypeArray}, true
	}

	elements := make([]RespValue, len(args))
	for i, arg := range args {
		elements[i] = RespValue{Type: TypeBulkString, Bulk: arg}
	}
	return RespValue{Type: TypeArray, Array: elements}, true
}

func (p *Parser) parseInlineArgs(line []byte) [][]byte {
	var args [][]byte
	var quote bool
	var quotech byte
	var escape bool

outer:
	for len(line) > 0 {
		nline := make([]byte, 0, len(line))
		for i := 0; i < len(line); i++ {
			c := line[i]
			if !quote {
				if c == ' ' {
					if len(nline) > 0 {
						args = append(args, nline)
					}
					line = line[i+1:]
					continue outer
				}
				if c == '"' || c == '\'' {
					if i != 0 {
						return nil
					}
					quotech = c
					quote = true
					line = line[i+1:]
					continue outer
				}
			} else {
				if escape {
					escape = false
					switch c {
					case 'n':
						c = '\n'
					case 'r':
						c = '\r'
					case 't':
						c = '\t'
					}
				} else if c == quotech {
					quote = false
					quotech = 0
					args = append(args, nline)
					line = line[i+1:]
					if len(line) > 0 && line[0] != ' ' {
						return nil
					}
					continue outer
				} else if c == '\\' {
					escape = true
					continue
				}
			}
			nline = append(nline, c)
		}
		if quote {
			return nil
		}
		if len(line) > 0 {
			args = append(args, line)
		}
		break
	}

	return args
}

func (p *Parser) findCRLF(pos int) int {
	for i := pos; i < len(p.buf)-1; i++ {
		if p.buf[i] == '\r' && p.buf[i+1] == '\n' {
			return i + 1
		}
	}
	return -1
}

func parseIntBuf(b []byte) (int, bool) {
	if len(b) == 0 {
		return 0, false
	}
	if len(b) == 1 && b[0] >= '0' && b[0] <= '9' {
		return int(b[0] - '0'), true
	}

	var n int
	var sign bool
	var i int

	if b[0] == '-' {
		sign = true
		i++
	} else if b[0] == '+' {
		i++
	}

	if i >= len(b) {
		return 0, false
	}

	for ; i < len(b); i++ {
		if b[i] < '0' || b[i] > '9' {
			return 0, false
		}
		n = n*10 + int(b[i]-'0')
	}

	if sign {
		n *= -1
	}
	return n, true
}

func parseInt64(b []byte) (int64, bool) {
	if len(b) == 0 {
		return 0, false
	}
	if len(b) == 1 && b[0] >= '0' && b[0] <= '9' {
		return int64(b[0] - '0'), true
	}

	var n int64
	var sign bool
	var i int

	if b[0] == '-' {
		sign = true
		i++
	} else if b[0] == '+' {
		i++
	}

	if i >= len(b) {
		return 0, false
	}

	for ; i < len(b); i++ {
		if b[i] < '0' || b[i] > '9' {
			return 0, false
		}
		n = n*10 + int64(b[i]-'0')
	}

	if sign {
		n *= -1
	}
	return n, true
}
