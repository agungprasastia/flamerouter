package executor

import (
	"encoding/binary"
	"math"
)

// EncodeCursorProtobuf encodes JSON body into Cursor's protobuf wire format.
// Field 1 (string): tag 0x0a + varint length + data.
func EncodeCursorProtobuf(body []byte) []byte {
	tag := byte(0x0a)
	length := len(body)
	buf := make([]byte, 0, 1+binary.MaxVarintLen64+length)
	buf = append(buf, tag)

	var lenBuf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(lenBuf[:], uint64(length))
	buf = append(buf, lenBuf[:n]...)
	buf = append(buf, body...)

	return buf
}

// DecodeCursorProtobuf extracts JSON from Cursor's protobuf response.
func DecodeCursorProtobuf(data []byte) []byte {
	if len(data) < 2 {
		return nil
	}
	// skip tag
	i := 1

	length, n := binary.Uvarint(data[i:])
	if n <= 0 || length > uint64(len(data)) || length > math.MaxInt {
		return nil
	}

	i += n
	lInt := int(length)

	end := i + lInt
	if end > len(data) || end < i {
		return nil
	}

	out := make([]byte, lInt)
	copy(out, data[i:end])

	return out
}
