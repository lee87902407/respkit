package resp

import (
	"errors"
)

var (
	errIncompleteCommand      = errors.New("incomplete command")
	errInvalidBulkLength      = errors.New("invalid bulk length")
	errInvalidMultiBulkLength = errors.New("invalid multibulk length")
	errProtocol               = errors.New("protocol error")
)

// Parser implements zero-copy RESP parsing
// The parser returns slices into the input buffer, not allocating new memory
// for bulk strings and arrays. Caller owns the buffer and must ensure it
// remains valid for the duration of command processing.
type Parser struct {
	buf   []byte // Read buffer (owned by caller)
	pos   int    // Current read position
	marks []int  // Mark positions for slicing array elements
}

// NewParser creates a new zero-copy RESP parser
func NewParser(buf []byte) *Parser {
	return &Parser{
		buf:   buf,
		pos:   0,
		marks: make([]int, 0, 16),
	}
}

// Reset resets the parser with a new buffer
func (p *Parser) Reset(buf []byte) {
	p.buf = buf
	p.pos = 0
	p.marks = p.marks[:0]
}

// Position returns the current parse position in the buffer.
func (p *Parser) Position() int {
	return p.pos
}

// ParseNextCommand parses the next command from the buffer
// Returns ParseResult with slices referencing the input buffer (zero-copy)
// If command is incomplete, returns Complete=false
func (p *Parser) ParseNextCommand() ParseResult {
	if p.pos >= len(p.buf) {
		return ParseResult{Complete: false}
	}

	// Check first byte to determine parsing path
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
		// Inline telnet command (plain text)
		return p.parseInlineCommand()
	}
}

// parseArray parses RESP arrays: *<count>\r\n<bulk1><bulk2>...
// Zero-copy: returns slices referencing input buffer
func (p *Parser) parseArray() ParseResult {
	start := p.pos
	p.marks = p.marks[:0]

	// Find end of array count line
	lineEnd := p.findCRLF(p.pos + 1)
	if lineEnd == -1 {
		return ParseResult{Complete: false}
	}

	// Parse array count
	count, ok := parseInt(p.buf[p.pos+1 : lineEnd-1]) // Skip '*' and '\r'
	if !ok || count < 0 {
		return ParseResult{Complete: false}
	}

	p.pos = lineEnd + 1 // Skip past '\n'

	// Parse each bulk string in the array
	for i := 0; i < count; i++ {
		if p.pos >= len(p.buf) {
			return ParseResult{Complete: false}
		}

		// Expect '$'
		if p.buf[p.pos] != '$' {
			return ParseResult{Complete: false}
		}

		// Find end of bulk length line
		bulkLineEnd := p.findCRLF(p.pos + 1)
		if bulkLineEnd == -1 {
			return ParseResult{Complete: false}
		}

		// Parse bulk length
		bulkLen, ok := parseInt(p.buf[p.pos+1 : bulkLineEnd-1])
		if !ok || bulkLen < 0 {
			return ParseResult{Complete: false}
		}

		dataStart := bulkLineEnd + 1 // Start of bulk data
		dataEnd := dataStart + bulkLen

		// Check if we have complete bulk data + CRLF
		if dataEnd+2 > len(p.buf) {
			return ParseResult{Complete: false}
		}

		// Verify CRLF after bulk data
		if p.buf[dataEnd] != '\r' || p.buf[dataEnd+1] != '\n' {
			return ParseResult{Complete: false}
		}

		// Mark start and end positions for this arg (zero-copy)
		p.marks = append(p.marks, dataStart, dataEnd)

		p.pos = dataEnd + 2 // Move to next element
	}

	// Successfully parsed complete array
	endPos := p.pos
	result := ParseResult{
		Type:     Array,
		Raw:      p.buf[start:endPos], // Zero-copy slice
		Complete: true,
	}

	// Build args from marks (zero-copy slices into Raw)
	result.Args = make([][]byte, len(p.marks)/2)
	for i := 0; i < len(p.marks); i += 2 {
		result.Args[i/2] = p.buf[p.marks[i]:p.marks[i+1]]
	}

	return result
}

// parseSimpleString parses RESP simple strings: +<string>\r\n
// Zero-copy: returns slice referencing input buffer
func (p *Parser) parseSimpleString() ParseResult {
	start := p.pos
	lineEnd := p.findCRLF(p.pos + 1)
	if lineEnd == -1 {
		return ParseResult{Complete: false}
	}

	p.pos = lineEnd + 1

	return ParseResult{
		Type:     SimpleString,
		Raw:      p.buf[start:p.pos],                   // Zero-copy slice
		Args:     [][]byte{p.buf[start+1 : lineEnd-1]}, // Skip '+' and '\r'
		Complete: true,
	}
}

// parseError parses RESP errors: -<string>\r\n
// Zero-copy: returns slice referencing input buffer
func (p *Parser) parseError() ParseResult {
	start := p.pos
	lineEnd := p.findCRLF(p.pos + 1)
	if lineEnd == -1 {
		return ParseResult{Complete: false}
	}

	p.pos = lineEnd + 1

	return ParseResult{
		Type:     Error,
		Raw:      p.buf[start:p.pos],                   // Zero-copy slice
		Args:     [][]byte{p.buf[start+1 : lineEnd-1]}, // Skip '-' and '\r'
		Complete: true,
	}
}

// parseInt parses RESP integers: :<number>\r\n
// Zero-copy: returns slice referencing input buffer
func (p *Parser) parseInt() ParseResult {
	start := p.pos
	lineEnd := p.findCRLF(p.pos + 1)
	if lineEnd == -1 {
		return ParseResult{Complete: false}
	}

	p.pos = lineEnd + 1

	return ParseResult{
		Type:     Integer,
		Raw:      p.buf[start:p.pos],                   // Zero-copy slice
		Args:     [][]byte{p.buf[start+1 : lineEnd-1]}, // Skip ':' and '\r'
		Complete: true,
	}
}

// parseBulkString parses RESP bulk strings: $<len>\r\n<data>\r\n
// Zero-copy: returns slice referencing input buffer
func (p *Parser) parseBulkString() ParseResult {
	start := p.pos

	// Find end of bulk length line
	lineEnd := p.findCRLF(p.pos + 1)
	if lineEnd == -1 {
		return ParseResult{Complete: false}
	}

	// Parse bulk length
	bulkLen, ok := parseInt(p.buf[p.pos+1 : lineEnd-1]) // Skip '$' and '\r'
	if !ok {
		return ParseResult{Complete: false}
	}

	// Handle null bulk strings ($-1\r\n)
	if bulkLen == -1 {
		p.pos = lineEnd + 1
		return ParseResult{
			Type:     BulkString,
			Raw:      p.buf[start:p.pos], // Zero-copy slice
			Args:     nil,                // Null bulk string
			Complete: true,
		}
	}

	if bulkLen < 0 {
		return ParseResult{Complete: false}
	}

	dataStart := lineEnd + 1 // Start of bulk data
	dataEnd := dataStart + bulkLen

	// Check if we have complete bulk data + CRLF
	if dataEnd+2 > len(p.buf) {
		return ParseResult{Complete: false}
	}

	// Verify CRLF after bulk data
	if p.buf[dataEnd] != '\r' || p.buf[dataEnd+1] != '\n' {
		return ParseResult{Complete: false}
	}

	p.pos = dataEnd + 2

	return ParseResult{
		Type:     BulkString,
		Raw:      p.buf[start:p.pos],                 // Zero-copy slice
		Args:     [][]byte{p.buf[dataStart:dataEnd]}, // Zero-copy data slice
		Complete: true,
	}
}

// parseInlineCommand parses inline telnet-style commands
// These require allocation for transformation to RESP format
func (p *Parser) parseInlineCommand() ParseResult {
	// Find newline
	newlinePos := p.findCRLF(p.pos)
	if newlinePos == -1 {
		return ParseResult{Complete: false}
	}

	// Extract line (without CRLF)
	var line []byte
	if newlinePos > p.pos && p.buf[newlinePos-1] == '\r' {
		line = p.buf[p.pos : newlinePos-1]
	} else {
		line = p.buf[p.pos:newlinePos]
	}

	// Parse quoted strings and spaces
	args := p.parseInlineArgs(line)
	if len(args) == 0 {
		p.pos = newlinePos + 1
		return ParseResult{Complete: true} // Empty command
	}

	p.pos = newlinePos + 1

	// Note: This path DOES allocate because inline commands need
	// transformation to RESP format. This is expected and documented.
	// Zero-copy only applies to native RESP format commands.
	return ParseResult{
		Type:     Array,                             // Inline commands are converted to arrays
		Raw:      p.buf[p.pos-newlinePos-1 : p.pos], // View of original line
		Args:     args,
		Complete: true,
	}
}

// parseInlineArgs parses inline command arguments with quote support
// This allocates because inline commands need transformation
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
						return nil // Unbalanced quotes
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
						return nil // Unbalanced quotes
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
			return nil // Unbalanced quotes
		}
		if len(line) > 0 {
			args = append(args, line)
		}
		break
	}

	return args
}

// findCRLF finds the next CRLF sequence starting from pos
// Returns the position after '\n', or -1 if not found
func (p *Parser) findCRLF(pos int) int {
	for i := pos; i < len(p.buf)-1; i++ {
		if p.buf[i] == '\r' && p.buf[i+1] == '\n' {
			return i + 1 // Position after '\n'
		}
	}
	return -1
}

// parseInt parses a signed integer from bytes
// Optimized for zero-allocation parsing
func parseInt(b []byte) (int, bool) {
	if len(b) == 0 {
		return 0, false
	}

	// Fast path for single digit
	if len(b) == 1 && b[0] >= '0' && b[0] <= '9' {
		return int(b[0] - '0'), true
	}

	var n int
	var sign bool
	var i int

	// Handle sign
	if b[0] == '-' {
		sign = true
		i++
	} else if b[0] == '+' {
		i++
	}

	if i >= len(b) {
		return 0, false
	}

	// Parse digits
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
