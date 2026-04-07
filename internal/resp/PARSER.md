# Zero-Copy RESP Parser Implementation

## Overview

The RESP parser in `internal/resp/parser.go` implements zero-copy parsing for optimal performance. When parsing RESP protocol messages from a pre-read byte buffer, the parser returns slices that reference the original buffer rather than allocating new memory for bulk strings and arrays.

## Performance Characteristics

### Benchmark Results (Apple M4)

```
BenchmarkParseArray-10              50.30 ns/op    208 B/op    2 allocs/op
BenchmarkParseBulkString-10         43.92 ns/op    152 B/op    2 allocs/op
BenchmarkParseMultipleArrays-10     81.08 ns/op    256 B/op    3 allocs/op
BenchmarkParseSimpleString-10       43.75 ns/op    152 B/op    2 allocs/op
BenchmarkParseInteger-10            45.06 ns/op    152 B/op    2 allocs/op
```

### Allocation Breakdown

The **2 allocations per operation** are minimal:
1. **ParseResult struct** (~64 bytes) - Returned by `ParseNextCommand()`
2. **result.Args slice** (~24-88 bytes depending on array size) - Holds references to bulk string data

**Critical**: Zero-copy means bulk string data is NEVER copied. The `result.Args` slices point directly into the input buffer.

## Zero-Copy Guarantee

```go
// Input buffer (owned by caller)
buf := []byte("*3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$5\r\nvalue\r\n")

// Parse returns slices into buf, NOT copies
parser := NewParser(buf)
result := parser.ParseNextCommand()

// result.Args[0] points into buf, no allocation for "SET" string
// result.Args[1] points into buf, no allocation for "key" string
// result.Args[2] points into buf, no allocation for "value" string
```

## Buffer Ownership Model

**CRITICAL**: The caller owns the buffer and must ensure it remains valid for the duration of command processing.

```go
// CORRECT: Buffer lives longer than command processing
func handleConnection(conn net.Conn) {
    buf := make([]byte, 4096)
    n, _ := conn.Read(buf)
    
    parser := NewParser(buf[:n])
    result := parser.ParseNextCommand()
    
    // result.Args references buf - safe to use here
    processCommand(result)
    
    // buf is still valid here
}

// INCORRECT: Buffer dies before command processing
func dangerous() {
    result := parseFromTemporaryBuffer()
    processCommand(result)  // CRASH: result.Args point to freed memory
}

func parseFromTemporaryBuffer() ParseResult {
    buf := []byte("*2\r\n$3\r\nGET\r\n$3\r\nkey\r\n")  // Allocated on stack
    parser := NewParser(buf)
    return parser.ParseNextCommand()  // Returns slices into buf
    // buf is freed when function returns
}
```

## Implementation Details

### Parser State

```go
type Parser struct {
    buf    []byte // Read buffer (owned by caller, never copied)
    pos    int    // Current read position
    marks  []int  // Mark positions for slicing array elements (reusable)
}
```

### Zero-Copy Array Parsing

The key to zero-copy parsing is recording positions and creating slices:

```go
// Parse array: *3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$5\r\nvalue\r\n

// 1. Record start position
start := p.pos  // 0

// 2. For each bulk string, record data start and end
marks = append(marks, dataStart, dataEnd)
// marks = [10, 13, 17, 20, 25, 30] for SET, key, value

// 3. Create slices from marks
result.Args = make([][]byte, len(marks)/2)  // Only allocation
for i := 0; i < len(marks); i += 2 {
    result.Args[i/2] = p.buf[marks[i]:marks[i+1]]  // Zero-copy slice
}

// result.Args[0] = buf[10:13]  -> "SET"
// result.Args[1] = buf[17:20]  -> "key"
// result.Args[2] = buf[25:30]  -> "value"
```

### Allocation Exceptions

Only **inline telnet commands** require allocation for transformation to RESP format:

```go
// Inline command: SET key value
// Requires transformation to: *3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$5\r\nvalue\r\n
// This path DOES allocate (expected and documented)
```

## Supported RESP Types

- **Arrays** (`*<count>\r\n...`) - Zero-copy
- **Bulk Strings** (`$<len>\r\n<data>\r\n`) - Zero-copy
- **Simple Strings** (`+<string>\r\n`) - Zero-copy
- **Errors** (`-<string>\r\n`) - Zero-copy
- **Integers** (`:<number>\r\n`) - Zero-copy
- **Inline Commands** - Requires allocation (transformation)

## Usage Example

```go
package main

import (
    "bufio"
    "net"
    "github.com/yanjie/netgo/internal/resp"
)

func handleConnection(conn net.Conn) {
    defer conn.Close()
    rd := bufio.NewReader(conn)
    
    for {
        // Read into buffer (owned by connection handler)
        buf := make([]byte, 4096)
        n, err := rd.Read(buf)
        if err != nil {
            break
        }
        
        // Parse with zero-copy
        parser := resp.NewParser(buf[:n])
        result := parser.ParseNextCommand()
        
        if result.Complete {
            // result.Args are zero-copy slices into buf
            cmd := string(result.Args[0])
            args := result.Args[1:]
            
            // Process command (buf must remain valid)
            processCommand(cmd, args)
        }
    }
}

func processCommand(cmd string, args [][]byte) {
    switch cmd {
    case "GET":
        key := string(args[0])  // Zero-copy slice
        // ... handle GET
    case "SET":
        key := string(args[0])
        value := string(args[1])
        // ... handle SET
    }
}
```

## Performance Impact

### Memory Savings

For a typical SET command with 100-byte value:
- **Traditional parser**: Allocates ~100 bytes for value string
- **Zero-copy parser**: Allocates 0 bytes for value (just a slice header)

### Scalability

The zero-copy approach provides:
- **Reduced GC pressure** - Fewer allocations to collect
- **Better cache locality** - Data stays in original buffer
- **Predictable memory usage** - Allocation count independent of payload size

## Verification

Run tests to verify zero-copy behavior:

```bash
# Run all tests
go test -v ./internal/resp/

# Run benchmarks
go test -bench=. -benchmem ./internal/resp/

# Verify zero-copy with memory profiling
go test -run TestZeroCopyAllocation -v ./internal/resp/
```

## Reference Implementation

This implementation is based on the zero-copy pattern from [redcon](https://github.com/tidwall/redcon), specifically:
- `redcon.go:955-973` - Zero-copy slice assignment pattern
- `redcon.go:802-1010` - Command reading with buffer ownership model
