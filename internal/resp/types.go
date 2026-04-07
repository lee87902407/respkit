package resp

// RespType represents RESP protocol data types
type RespType byte

const (
	SimpleString RespType = '+'
	Error        RespType = '-'
	Integer      RespType = ':'
	BulkString   RespType = '$'
	Array        RespType = '*'
)

// ParseResult is a zero-copy view into the buffer
type ParseResult struct {
	Type     RespType
	Raw      []byte   // View into parser buffer (zero-copy)
	Args     [][]byte // Slices referencing Raw
	Complete bool
}
