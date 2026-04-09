package protocol

import (
	"bytes"
	"strings"
)

// RespType represents RESP protocol data types.
type RespType byte

const (
	TypeSimpleString RespType = '+'
	TypeError        RespType = '-'
	TypeInteger      RespType = ':'
	TypeBulkString   RespType = '$'
	TypeArray        RespType = '*'
	TypeNull         RespType = 0
)

// RespValue represents a complete RESP value.
// Bulk and Array elements may reference a Scope-managed buffer;
// their lifetime is controlled by the Scope.
type RespValue struct {
	Type  RespType
	Str   string      // SimpleString, Error
	Int   int64       // Integer
	Bulk  []byte      // BulkString (zero-copy, may reference Scope buffer)
	Array []RespValue // Array elements
}

// SimpleString creates a RESP simple string value.
func SimpleString(s string) RespValue {
	return RespValue{Type: TypeSimpleString, Str: s}
}

// Error creates a RESP error value.
func Error(msg string) RespValue {
	return RespValue{Type: TypeError, Str: msg}
}

// Integer creates a RESP integer value.
func Integer(n int64) RespValue {
	return RespValue{Type: TypeInteger, Int: n}
}

// BulkBytes creates a RESP bulk string value.
func BulkBytes(b []byte) RespValue {
	return RespValue{Type: TypeBulkString, Bulk: b}
}

// BulkFromString creates a RESP bulk string from a Go string.
func BulkFromString(s string) RespValue {
	return RespValue{Type: TypeBulkString, Bulk: []byte(s)}
}

// Null creates a RESP null value.
func Null() RespValue {
	return RespValue{Type: TypeNull}
}

// ArrayOf creates a RESP array from the given values.
func ArrayOf(values ...RespValue) RespValue {
	return RespValue{Type: TypeArray, Array: values}
}

// IsNull returns true if this value represents a RESP null.
func (v RespValue) IsNull() bool {
	return v.Type == TypeNull || (v.Type == TypeBulkString && v.Bulk == nil)
}

// CommandName extracts the command name from an Array type RespValue.
// Returns the first element lowercased, or empty string if not an array.
func (v RespValue) CommandName() string {
	if v.Type != TypeArray || len(v.Array) == 0 {
		return ""
	}
	first := v.Array[0]
	var raw []byte
	switch first.Type {
	case TypeBulkString:
		raw = first.Bulk
	case TypeSimpleString:
		raw = []byte(first.Str)
	default:
		return ""
	}
	return strings.ToLower(string(raw))
}

// CommandArgs extracts command arguments from an Array type RespValue.
// Returns all elements after the first, as raw byte slices.
func (v RespValue) CommandArgs() [][]byte {
	if v.Type != TypeArray || len(v.Array) <= 1 {
		return nil
	}
	args := make([][]byte, 0, len(v.Array)-1)
	for _, elem := range v.Array[1:] {
		switch elem.Type {
		case TypeBulkString:
			args = append(args, elem.Bulk)
		case TypeSimpleString, TypeError:
			args = append(args, []byte(elem.Str))
		case TypeInteger:
			args = append(args, []byte(int64ToBytes(elem.Int)))
		default:
			args = append(args, elem.Bulk)
		}
	}
	return args
}

// Equal checks if two RespValues are equal.
func (v RespValue) Equal(other RespValue) bool {
	if v.Type != other.Type {
		return false
	}
	switch v.Type {
	case TypeSimpleString, TypeError:
		return v.Str == other.Str
	case TypeInteger:
		return v.Int == other.Int
	case TypeBulkString:
		return bytes.Equal(v.Bulk, other.Bulk)
	case TypeArray:
		if len(v.Array) != len(other.Array) {
			return false
		}
		for i := range v.Array {
			if !v.Array[i].Equal(other.Array[i]) {
				return false
			}
		}
		return true
	case TypeNull:
		return true
	default:
		return false
	}
}

func int64ToBytes(n int64) []byte {
	if n == 0 {
		return []byte{'0'}
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return buf[i:]
}
