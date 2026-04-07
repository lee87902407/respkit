package resp

import (
	"errors"
	"testing"
)

func TestWriterSimpleError(t *testing.T) {
	w := NewWriter(128)
	err := errors.New("not found")
	w.AppendAny(err)
	result := string(w.Bytes())
	// Regular errors should get the "ERR " prefix
	if result != "-ERR not found\r\n" {
		t.Errorf("Expected \"-ERR not found\\r\\n\", got %q", result)
	}

	// AnySimpleError should NOT get the "ERR " prefix
	w2 := NewWriter(128)
	w2.AppendAny(AnySimpleError{err})
	result2 := string(w2.Bytes())
	if result2 != "-not found\r\n" {
		t.Errorf("Expected \"-not found\\r\\n\", got %q", result2)
	}
}
