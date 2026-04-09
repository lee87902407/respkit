package protocol

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Writer serializes RespValue elements into RESP wire format.
type Writer struct {
	buf []byte
}

// NewWriter creates a new Writer with the given initial buffer size.
func NewWriter(initialSize int) *Writer {
	return &Writer{
		buf: make([]byte, 0, initialSize),
	}
}

// Write appends the RESP encoding of v to the internal buffer.
func (w *Writer) Write(v RespValue) error {
	w.Serialize(v)
	return nil
}

// Serialize appends the RESP encoding of v to the internal buffer
// and returns the serialized bytes.
func (w *Writer) Serialize(v RespValue) []byte {
	w.buf = w.appendValue(w.buf, v)
	return w.buf
}

// Bytes returns the current buffer contents.
func (w *Writer) Bytes() []byte {
	return w.buf
}

// Reset clears the buffer, retaining capacity.
func (w *Writer) Reset() {
	w.buf = w.buf[:0]
}

// Len returns the current buffer length.
func (w *Writer) Len() int {
	return len(w.buf)
}

// Flush writes the buffered data to dst and clears the buffer.
func (w *Writer) Flush(dst io.Writer) error {
	if len(w.buf) == 0 {
		return nil
	}
	_, err := dst.Write(w.buf)
	w.Reset()
	if err != nil {
		return fmt.Errorf("protocol: flush writer: %w", err)
	}
	return nil
}

func (w *Writer) appendValue(b []byte, v RespValue) []byte {
	switch v.Type {
	case TypeSimpleString:
		return w.appendSimpleString(b, v.Str)
	case TypeError:
		return w.appendError(b, v.Str)
	case TypeInteger:
		return w.appendPrefix(b, ':', v.Int)
	case TypeBulkString:
		if v.Bulk == nil {
			return append(b, '$', '-', '1', '\r', '\n')
		}
		return w.appendBulk(b, v.Bulk)
	case TypeArray:
		if v.Array == nil {
			return append(b, '*', '-', '1', '\r', '\n')
		}
		b = w.appendPrefix(b, '*', int64(len(v.Array)))
		for _, elem := range v.Array {
			b = w.appendValue(b, elem)
		}
		return b
	case TypeNull:
		return append(b, '$', '-', '1', '\r', '\n')
	default:
		return append(b, '$', '-', '1', '\r', '\n')
	}
}

func (w *Writer) appendSimpleString(b []byte, s string) []byte {
	b = append(b, '+')
	b = append(b, stripNewlines(s)...)
	return append(b, '\r', '\n')
}

func (w *Writer) appendError(b []byte, s string) []byte {
	b = append(b, '-')
	b = append(b, stripNewlines(s)...)
	return append(b, '\r', '\n')
}

func (w *Writer) appendBulk(b []byte, data []byte) []byte {
	b = w.appendPrefix(b, '$', int64(len(data)))
	b = append(b, data...)
	return append(b, '\r', '\n')
}

func (w *Writer) appendPrefix(b []byte, c byte, n int64) []byte {
	if n >= 0 && n <= 9 {
		return append(b, c, byte('0'+n), '\r', '\n')
	}
	b = append(b, c)
	b = strconv.AppendInt(b, n, 10)
	return append(b, '\r', '\n')
}

// SerializeValue is a convenience function that serializes a single RespValue.
func SerializeValue(v RespValue) []byte {
	w := NewWriter(64)
	return w.Serialize(v)
}

func stripNewlines(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\r' || s[i] == '\n' {
			s = strings.Replace(s, "\r", " ", -1)
			s = strings.Replace(s, "\n", " ", -1)
			break
		}
	}
	return s
}
