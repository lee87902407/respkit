package resp

import (
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
)

// Writer provides buffered RESP response writing with manual flush control.
// Buffer grows as needed and responses are accumulated until Flush is called.
type Writer struct {
	buf    []byte // Write buffer
	offset int    // Current write position
}

// NewWriter creates a new RESP writer with the specified initial buffer size.
// Size should be chosen based on expected response sizes.
// Common starting sizes: 128 (small), 512 (medium), 4096 (large).
func NewWriter(size int) *Writer {
	return &Writer{
		buf: make([]byte, 0, size),
	}
}

// AppendBulk writes a RESP bulk string: "$<len>\r\n<data>\r\n"
// Example: "hello" -> "$5\r\nhello\r\n"
func (w *Writer) AppendBulk(data []byte) {
	w.buf = w.appendBulk(w.buf, data)
}

// appendBulk is the internal implementation that returns the updated buffer.
func (w *Writer) appendBulk(b []byte, bulk []byte) []byte {
	b = w.appendPrefix(b, '$', int64(len(bulk)))
	b = append(b, bulk...)
	return append(b, '\r', '\n')
}

// AppendArray writes a RESP array header: "*<count>\r\n"
// Follow with array elements using other Append* methods.
// Example: 2 elements -> "*2\r\n"
func (w *Writer) AppendArray(count int) {
	w.buf = w.appendPrefix(w.buf, '*', int64(count))
}

// AppendError writes a RESP error: "-<msg>\r\n"
// Error messages should not contain newlines (they will be replaced with spaces).
// Example: "ERR not found" -> "-ERR not found\r\n"
func (w *Writer) AppendError(msg string) {
	w.buf = w.appendError(w.buf, msg)
}

// appendError is the internal implementation that returns the updated buffer.
func (w *Writer) appendError(b []byte, s string) []byte {
	b = append(b, '-')
	b = append(b, w.stripNewlines(s)...)
	return append(b, '\r', '\n')
}

// AppendString writes a RESP simple string: "+<msg>\r\n"
// Simple strings should not contain newlines (they will be replaced with spaces).
// Example: "OK" -> "+OK\r\n"
func (w *Writer) AppendString(msg string) {
	w.buf = w.appendString(w.buf, msg)
}

// appendString is the internal implementation that returns the updated buffer.
func (w *Writer) appendString(b []byte, s string) []byte {
	b = append(b, '+')
	b = append(b, w.stripNewlines(s)...)
	return append(b, '\r', '\n')
}

// AppendInt writes a RESP integer: ":<n>\r\n"
// Example: 42 -> ":42\r\n"
func (w *Writer) AppendInt(n int64) {
	w.buf = w.appendPrefix(w.buf, ':', n)
}

// AppendNull writes a RESP null: "$-1\r\n"
func (w *Writer) AppendNull() {
	w.buf = append(w.buf, '$', '-', '1', '\r', '\n')
}

// AppendAny writes any Go type as an appropriate RESP type:
//   - nil -> Null
//   - error -> Error (adds "ERR " prefix if first word is not uppercase)
//   - string -> BulkString
//   - []byte -> BulkString (nil becomes Null)
//   - bool -> BulkString ("0" or "1")
//   - int/int8/int16/int32/int64 -> BulkInt
//   - uint/uint8/uint16/uint32/uint64 -> BulkUint
//   - float32/float64 -> BulkFloat
//   - []any -> Array (recursive)
//   - map[string]any -> Array of key-value pairs (recursive)
//   - AnySimpleString -> SimpleString (wrapper type)
//   - AnySimpleInt -> Integer (wrapper type)
//   - AnySimpleError -> Error without "ERR" prefix (wrapper type)
//   - everything else -> BulkString using fmt.Sprint()
func (w *Writer) AppendAny(v interface{}) []byte {
	w.buf = w.appendAny(w.buf, v)
	return w.buf
}

// appendAny is the internal implementation that returns the updated buffer.
func (w *Writer) appendAny(b []byte, v interface{}) []byte {
	switch v := v.(type) {
	case AnySimpleString:
		b = w.appendString(b, string(v))
	case AnySimpleInt:
		b = w.appendPrefix(b, ':', int64(v))
	case AnySimpleError:
		b = w.appendError(b, v.Err.Error())
	case nil:
		b = append(b, '$', '-', '1', '\r', '\n')
	case error:
		msg := w.prefixERRIfNeeded(v.Error())
		b = w.appendError(b, msg)
	case string:
		b = w.appendBulkString(b, v)
	case []byte:
		if v == nil {
			b = append(b, '$', '-', '1', '\r', '\n')
		} else {
			b = w.appendBulk(b, v)
		}
	case bool:
		if v {
			b = w.appendBulkString(b, "1")
		} else {
			b = w.appendBulkString(b, "0")
		}
	case int:
		b = w.appendBulkInt(b, int64(v))
	case int8:
		b = w.appendBulkInt(b, int64(v))
	case int16:
		b = w.appendBulkInt(b, int64(v))
	case int32:
		b = w.appendBulkInt(b, int64(v))
	case int64:
		b = w.appendBulkInt(b, v)
	case uint:
		b = w.appendBulkUint(b, uint64(v))
	case uint8:
		b = w.appendBulkUint(b, uint64(v))
	case uint16:
		b = w.appendBulkUint(b, uint64(v))
	case uint32:
		b = w.appendBulkUint(b, uint64(v))
	case uint64:
		b = w.appendBulkUint(b, v)
	case float32:
		b = w.appendBulkFloat(b, float64(v))
	case float64:
		b = w.appendBulkFloat(b, v)
	default:
		vv := reflect.ValueOf(v)
		switch vv.Kind() {
		case reflect.Slice:
			n := vv.Len()
			b = w.appendPrefix(b, '*', int64(n))
			for i := 0; i < n; i++ {
				b = w.appendAny(b, vv.Index(i).Interface())
			}
		case reflect.Map:
			keys := vv.MapKeys()
			n := len(keys)
			b = w.appendPrefix(b, '*', int64(n*2))
			for _, key := range keys {
				keyStr := fmt.Sprint(key.Interface())
				b = w.appendBulkString(b, keyStr)
				b = w.appendAny(b, vv.MapIndex(key).Interface())
			}
		default:
			b = w.appendBulkString(b, fmt.Sprint(v))
		}
	}
	return b
}

// Flush writes the buffered data to the underlying connection and clears the buffer.
// Returns any error encountered during the write.
func (w *Writer) Flush(conn io.Writer) error {
	if len(w.buf) == 0 {
		return nil
	}
	_, err := conn.Write(w.buf)
	w.Reset()
	return err
}

// Bytes returns the current buffer contents without copying.
// The returned slice is valid until the next write or Reset.
func (w *Writer) Bytes() []byte {
	return w.buf
}

// Reset clears the buffer but retains the underlying capacity for reuse.
func (w *Writer) Reset() {
	w.buf = w.buf[:0]
	w.offset = 0
}

// appendPrefix writes a RESP prefix with type character and count/length.
// Optimized for single-digit values (0-9) to avoid allocations.
// Example: appendPrefix(buf, '$', 5) -> "$5\r\n"
func (w *Writer) appendPrefix(b []byte, c byte, n int64) []byte {
	if n >= 0 && n <= 9 {
		// Fast path for single digits
		return append(b, c, byte('0'+n), '\r', '\n')
	}
	b = append(b, c)
	b = strconv.AppendInt(b, n, 10)
	return append(b, '\r', '\n')
}

// appendBulkString writes a string as a bulk string.
func (w *Writer) appendBulkString(b []byte, bulk string) []byte {
	b = w.appendPrefix(b, '$', int64(len(bulk)))
	b = append(b, bulk...)
	return append(b, '\r', '\n')
}

// appendBulkInt writes an int64 as a bulk string.
func (w *Writer) appendBulkInt(b []byte, x int64) []byte {
	return w.appendBulk(b, strconv.AppendInt(nil, x, 10))
}

// appendBulkUint writes a uint64 as a bulk string.
func (w *Writer) appendBulkUint(b []byte, x uint64) []byte {
	return w.appendBulk(b, strconv.AppendUint(nil, x, 10))
}

// appendBulkFloat writes a float64 as a bulk string.
func (w *Writer) appendBulkFloat(b []byte, f float64) []byte {
	return w.appendBulk(b, []byte(strconv.AppendFloat(nil, f, 'f', -1, 64)))
}

// stripNewlines replaces newlines with spaces in strings.
// RESP simple strings and errors cannot contain raw newlines.
func (w *Writer) stripNewlines(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\r' || s[i] == '\n' {
			s = strings.Replace(s, "\r", " ", -1)
			s = strings.Replace(s, "\n", " ", -1)
			break
		}
	}
	return s
}

// prefixERRIfNeeded adds "ERR " prefix to error messages if the first word is not uppercase.
// Redis protocol requires error types to be uppercase (e.g., "ERR", "WRONGTYPE").
func (w *Writer) prefixERRIfNeeded(msg string) string {
	msg = strings.TrimSpace(msg)
	firstWord := strings.Split(msg, " ")[0]
	addERR := len(firstWord) == 0
	for i := 0; i < len(firstWord); i++ {
		if firstWord[i] < 'A' || firstWord[i] > 'Z' {
			addERR = true
			break
		}
	}
	if addERR {
		msg = strings.TrimSpace("ERR " + msg)
	}
	return msg
}

// Wrapper types for AppendAny to control RESP representation.

// AnySimpleString represents a value that should be written as a RESP simple string.
type AnySimpleString string

// AnySimpleInt represents a value that should be written as a RESP integer.
type AnySimpleInt int

// AnySimpleError wraps an error to be written as a RESP error without "ERR" prefix.
type AnySimpleError struct {
	Err error
}

// Error implements the error interface.
func (e AnySimpleError) Error() string {
	return e.Err.Error()
}
