package usage

import (
	"encoding/binary"
	"math"
	"time"
)

const (
	fieldCreditsInfo           = 1
	creditsFieldUsageRatio     = 1
	creditsFieldResetTimestamp = 5
	timestampFieldSeconds      = 1
	timestampFieldNanos        = 2

	wireTypeVarint          = 0
	wireTypeFixed64         = 1
	wireTypeLengthDelimited = 2
	wireTypeFixed32         = 5

	grpcWebTrailerFlagBit = 0x80
)

type protoField struct {
	bytes    []byte
	wireType int
	varint   uint64
}

func probeFrameHeader(buf []byte, offset int) (flag byte, payloadStart int, payloadLength int, ok bool) {
	if offset < 0 || len(buf)-offset < 5 {
		return 0, 0, 0, false
	}

	flag = buf[offset]
	if flag != 0x00 && flag != 0x01 && flag != 0x80 && flag != 0x81 {
		return 0, 0, 0, false
	}

	payloadStart = offset + 5
	payloadLength = int(binary.BigEndian.Uint32(buf[offset+1 : offset+5]))

	if payloadLength < 0 || payloadLength > len(buf)-payloadStart {
		return 0, 0, 0, false
	}

	return flag, payloadStart, payloadLength, true
}

func readVarint(buf []byte, offset int) (uint64, int, bool) {
	var result uint64

	var shift uint

	pos := offset

	for {
		if pos >= len(buf) {
			return 0, 0, false
		}

		b := buf[pos]
		result |= uint64(b&0x7f) << shift
		pos++

		if (b & 0x80) == 0 {
			break
		}

		shift += 7
		if shift > 70 {
			return 0, 0, false
		}
	}

	return result, pos, true
}

func readField(buf []byte, offset int) (int, protoField, int, bool) {
	tag, next, ok := readVarint(buf, offset)
	if !ok {
		return 0, protoField{}, 0, false
	}

	fieldNumber := int(tag >> 3)
	wireType := int(tag & 0x7)

	if fieldNumber == 0 {
		return 0, protoField{}, 0, false
	}

	switch wireType {
	case wireTypeVarint:
		val, nxt, vok := readVarint(buf, next)
		if !vok {
			return 0, protoField{}, 0, false
		}

		return fieldNumber, protoField{wireType: wireTypeVarint, varint: val}, nxt, true
	case wireTypeLengthDelimited:
		length, bodyStart, lok := readVarint(buf, next)
		if !lok || int(length) < 0 || bodyStart+int(length) > len(buf) {
			return 0, protoField{}, 0, false
		}

		return fieldNumber, protoField{
			wireType: wireTypeLengthDelimited,
			bytes:    buf[bodyStart : bodyStart+int(length)],
		}, bodyStart + int(length), true
	case wireTypeFixed64:
		if next+8 > len(buf) {
			return 0, protoField{}, 0, false
		}

		return fieldNumber, protoField{wireType: wireTypeFixed64, bytes: buf[next : next+8]}, next + 8, true
	case wireTypeFixed32:
		if next+4 > len(buf) {
			return 0, protoField{}, 0, false
		}

		return fieldNumber, protoField{wireType: wireTypeFixed32, bytes: buf[next : next+4]}, next + 4, true
	default:
		return 0, protoField{}, 0, false
	}
}

func decodeFields(buf []byte) (map[int]protoField, bool) {
	fields := make(map[int]protoField)

	offset := 0
	for offset < len(buf) {
		fieldNum, fld, next, ok := readField(buf, offset)
		if !ok {
			return nil, false
		}

		fields[fieldNum] = fld
		offset = next
	}

	return fields, true
}

func findDataFramePayload(buf []byte) []byte {
	offset := 0
	for offset < len(buf) {
		flag, pStart, pLen, ok := probeFrameHeader(buf, offset)
		if !ok {
			return nil
		}

		pEnd := pStart + pLen
		if (flag & grpcWebTrailerFlagBit) == 0 {
			return buf[pStart:pEnd]
		}

		offset = pEnd
	}

	return nil
}

func extractUsageRatio(fld protoField, exists bool) (float64, bool) {
	if !exists {
		return 0, true
	}

	if fld.wireType == wireTypeFixed32 && len(fld.bytes) == 4 {
		bits := binary.LittleEndian.Uint32(fld.bytes)
		return float64(math.Float32frombits(bits)), true
	}

	if fld.wireType == wireTypeFixed64 && len(fld.bytes) == 8 {
		bits := binary.LittleEndian.Uint64(fld.bytes)
		return math.Float64frombits(bits), true
	}

	return 0, false
}

func extractResetAt(fld protoField, exists bool) *string {
	if !exists || fld.wireType != wireTypeLengthDelimited {
		return nil
	}

	tsFields, ok := decodeFields(fld.bytes)
	if !ok {
		return nil
	}

	var seconds int64

	var nanos int64

	if s, has := tsFields[timestampFieldSeconds]; has && s.wireType == wireTypeVarint {
		seconds = int64(s.varint)
	}

	if n, has := tsFields[timestampFieldNanos]; has && n.wireType == wireTypeVarint {
		nanos = int64(n.varint)
	}

	millis := seconds*1000 + int64(math.Round(float64(nanos)/1000000.0))
	if millis <= 0 {
		return nil
	}

	t := time.UnixMilli(millis).UTC()
	iso := t.Format(time.RFC3339Nano)

	return &iso
}

func DecodeGrokCreditsFrame(buf []byte) (percentUsed float64, resetAt *string, ok bool) {
	if len(buf) == 0 {
		return 0, nil, false
	}

	_, _, _, isFramed := probeFrameHeader(buf, 0)
	payload := buf

	if isFramed {
		payload = findDataFramePayload(buf)
	}

	if payload == nil {
		return 0, nil, false
	}

	topFields, fok := decodeFields(payload)
	if !fok {
		return 0, nil, false
	}

	creditsFld, hasCredits := topFields[fieldCreditsInfo]
	if !hasCredits || creditsFld.wireType != wireTypeLengthDelimited {
		return 0, nil, false
	}

	nestedFields, nok := decodeFields(creditsFld.bytes)
	if !nok {
		return 0, nil, false
	}

	ratioFld, hasRatio := nestedFields[creditsFieldUsageRatio]

	ratio, rok := extractUsageRatio(ratioFld, hasRatio)
	if !rok || math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 {
		return 0, nil, false
	}

	pct := math.Min(100.0, ratio*100.0)
	resAt := extractResetAt(nestedFields[creditsFieldResetTimestamp], true)

	return pct, resAt, true
}
