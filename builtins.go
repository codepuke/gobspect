package gobspect

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// internalToUnix is the offset in seconds from Go's internal time epoch
// (Jan 1, year 1, 00:00:00 UTC) to the Unix epoch (Jan 1, 1970, 00:00:00 UTC).
// Computed as -(1969*365 + 1969/4 - 1969/100 + 1969/400) * 86400.
const internalToUnix int64 = -62135596800

// parseTimeBytes parses a time.Time GobEncoder/BinaryMarshaler blob into its
// components. Returns unixSec (seconds since Unix epoch), nsec (nanoseconds),
// and offsetSec (timezone offset in seconds east of UTC; 0 = UTC).
//
// Wire format (version 1, 15 bytes):
//
//	version(1) | internalSec(8 BE) | nsec(4 BE) | offsetMin(2 BE int16)
//
// Version 2 appends one additional byte for sub-minute timezone offsets.
// offsetMin == -1 is the sentinel for UTC.
// internalSec is seconds since Jan 1, year 1 (not Unix epoch).
func parseTimeBytes(data []byte) (unixSec int64, nsec int32, offsetSec int, err error) {
	if len(data) < 15 {
		return 0, 0, 0, fmt.Errorf("time.Time: too short: %d bytes", len(data))
	}
	version := data[0]
	wantLen := 15
	if version == 2 {
		wantLen = 16
	} else if version != 1 {
		return 0, 0, 0, fmt.Errorf("time.Time: unsupported version %d", version)
	}
	if len(data) != wantLen {
		return 0, 0, 0, fmt.Errorf("time.Time: invalid length %d for version %d", len(data), version)
	}

	sec := int64(data[1])<<56 | int64(data[2])<<48 | int64(data[3])<<40 | int64(data[4])<<32 |
		int64(data[5])<<24 | int64(data[6])<<16 | int64(data[7])<<8 | int64(data[8])
	nsec32 := int32(data[9])<<24 | int32(data[10])<<16 | int32(data[11])<<8 | int32(data[12])
	offsetMin := int16(data[13])<<8 | int16(data[14])

	// offsetMin == -1 is the sentinel for UTC; all others are minutes east of UTC.
	offSec := 0
	if offsetMin != -1 {
		offSec = int(offsetMin) * 60
	}
	if version == 2 {
		// stdlib UnmarshalBinary reads byte 15 as an unsigned byte (int(buf[2])),
		// not as int8. For negative sub-minute LMT offsets the encoder stores a
		// negative int8 value (e.g. -2 → 0xFE), but stdlib decodes it as the
		// positive value 254. We match that behaviour so that the reconstructed
		// zone offset is identical to what stdlib would return for the same blob.
		offSec += int(data[15])
	}

	// Convert from internal epoch (year 1) to Unix epoch (year 1970).
	return sec + internalToUnix, nsec32, offSec, nil
}

// decodeTime decodes a time.Time blob and returns it formatted as RFC 3339 with
// nanosecond precision. Use makeTimeDecoder for a custom layout.
func decodeTime(data []byte) (any, error) {
	unixSec, nsec, offsetSec, err := parseTimeBytes(data)
	if err != nil {
		return nil, err
	}
	return formatRFC3339(unixSec, nsec, offsetSec), nil
}

// makeTimeDecoder returns a [DecoderFunc] that parses a time.Time blob and
// formats the result using the given layout. If layout is empty,
// time.RFC3339Nano is used.
func makeTimeDecoder(layout string) DecoderFunc {
	if layout == "" {
		layout = time.RFC3339Nano
	}
	return func(data []byte) (any, error) {
		unixSec, nsec, offsetSec, err := parseTimeBytes(data)
		if err != nil {
			return nil, err
		}
		var loc *time.Location
		if offsetSec == 0 {
			loc = time.UTC
		} else {
			loc = time.FixedZone("", offsetSec)
		}
		t := time.Unix(unixSec, int64(nsec)).In(loc)
		return t.Format(layout), nil
	}
}

// formatRFC3339 formats a Unix timestamp as an RFC 3339 string with optional
// nanosecond precision. offsetSec is the total timezone offset in seconds east
// of UTC; 0 renders as "Z".
func formatRFC3339(unixSec int64, nsec int32, offsetSec int) string {
	y, mo, d, h, mi, s := secondsToCivil(unixSec + int64(offsetSec))

	var tz string
	if offsetSec == 0 {
		tz = "Z"
	} else {
		sign := '+'
		off := offsetSec
		if off < 0 {
			sign = '-'
			off = -off
		}
		tz = fmt.Sprintf("%c%02d:%02d", sign, off/3600, (off%3600)/60)
	}

	if nsec == 0 {
		return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d%s", y, mo, d, h, mi, s, tz)
	}
	frac := strings.TrimRight(fmt.Sprintf("%09d", nsec), "0")
	return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d.%s%s", y, mo, d, h, mi, s, frac, tz)
}

// secondsToCivil converts a Unix timestamp (optionally pre-adjusted for a
// timezone offset) to a civil date and time using the proleptic Gregorian
// calendar algorithm by Howard Hinnant.
func secondsToCivil(sec int64) (year, month, day, hour, min, second int) {
	tod := sec % 86400
	if tod < 0 {
		tod += 86400
	}
	days := (sec - tod) / 86400

	second = int(tod % 60)
	min = int((tod / 60) % 60)
	hour = int(tod / 3600)

	// Civil date from days since 1970-01-01.
	// Algorithm: https://howardhinnant.github.io/date_algorithms.html
	z := days + 719468
	var era int64
	if z >= 0 {
		era = z / 146097
	} else {
		era = (z - 146096) / 146097 // floor division for negative
	}
	doe := z - era*146097                                  // [0, 146096]
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365 // [0, 399]
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100) // [0, 365]
	mp := (5*doy + 2) / 153                  // [0, 11] (March = 0)
	d := doy - (153*mp+2)/5 + 1              // [1, 31]
	m := mp + 3
	if m > 12 { // Jan and Feb belong to the next year in this scheme
		m -= 12
		y++
	}
	return int(y), int(m), int(d), hour, min, second
}

// decodeBigInt decodes a math/big.Int GobEncoder blob.
//
// Wire format: (version<<1 | negBit) followed by big-endian absolute value bytes.
// Current version is 1. negBit=1 means the value is negative.
// Leading zero bytes in the absolute value are stripped by the encoder.
func decodeBigInt(data []byte) (any, error) {
	if len(data) == 0 {
		return "0", nil
	}
	b := data[0]
	if b>>1 != 1 {
		return nil, fmt.Errorf("big.Int: unsupported version %d", b>>1)
	}
	neg := b&1 != 0
	abs := data[1:]
	if len(abs) == 0 {
		return "0", nil
	}
	s := bigEndianToDecimal(abs)
	if neg {
		return "-" + s, nil
	}
	return s, nil
}

// decodeBigRat decodes a math/big.Rat GobEncoder blob.
//
// Wire format: (version<<1 | negBit) | uint32(numLen BE) | numAbs | denAbs.
// negBit applies to the numerator. The denominator is always non-negative.
func decodeBigRat(data []byte) (any, error) {
	if len(data) < 5 {
		return nil, fmt.Errorf("big.Rat: too short: %d bytes", len(data))
	}
	b := data[0]
	if b>>1 != 1 {
		return nil, fmt.Errorf("big.Rat: unsupported version %d", b>>1)
	}
	neg := b&1 != 0
	numLen := int(binary.BigEndian.Uint32(data[1:5]))
	if 5+numLen > len(data) {
		return nil, fmt.Errorf("big.Rat: numerator length %d exceeds data", numLen)
	}
	numAbs := data[5 : 5+numLen]
	denAbs := data[5+numLen:]

	var num string
	if len(numAbs) == 0 {
		num = "0"
	} else {
		num = bigEndianToDecimal(numAbs)
	}
	if neg {
		num = "-" + num
	}

	if len(denAbs) == 0 {
		return num, nil // zero Rat or integer Rat
	}
	den := bigEndianToDecimal(denAbs)
	if den == "1" {
		return num, nil
	}
	return num + "/" + den, nil
}

// decodeBigAuto dispatches between big.Int and big.Rat based on wire format.
//
// Both types have TypeName="" in the gob wire when encoded directly (pointer
// receivers cause gob to emit an empty CommonType.Name). This function is
// registered under the "" key and uses the following heuristic:
//
// big.Int strips leading zero bytes from the absolute value, so data[1] is
// always non-zero for any non-zero value. For big.Rat, data[1:5] is a
// big-endian uint32 numerator length, which is zero-padded and typically
// small (data[1] == 0x00 for practical values). The formats are therefore
// unambiguous in practice.
func decodeBigAuto(data []byte) (any, error) {
	if len(data) == 0 {
		return "0", nil
	}
	// Both big.Int and big.Rat use (version<<1)|negBit where version==1,
	// so data[0] must be 0x02 (positive/zero) or 0x03 (negative).
	// Reject any other value to avoid misinterpreting blobs from other
	// GobEncoder types (e.g. big.Float) that also have TypeName="" in the wire.
	if data[0] != 0x02 && data[0] != 0x03 {
		return nil, fmt.Errorf("gob: auto-detect: unexpected version/sign byte 0x%02x; not big.Int or big.Rat", data[0])
	}
	if len(data) >= 5 {
		numLen := int(binary.BigEndian.Uint32(data[1:5]))
		if numLen <= len(data)-5 {
			return decodeBigRat(data)
		}
	}
	return decodeBigInt(data)
}

// decodeBigFloat decodes a math/big.Float GobEncoder blob.
//
// Wire format (2-byte header + optional body):
//
//	byte 0: prec[12:8] (5 bits) | mode (3 bits)
//	byte 1: prec[7:0] (8 low bits of precision)
//
// Together bytes 0–1 carry 13 meaningful bits: prec_high(5), mode(3),
// acc(2), form(2), neg(1) packed into the 16-bit word alongside the low
// precision byte. If form is finite (form==1), bytes 2–5 are a big-endian
// int32 exponent followed by the mantissa words in big-endian order.
//
// Decoding is delegated to big.Float.GobDecode to avoid reimplementing the
// full wire format; the result is returned as a fixed-point decimal string.
func decodeBigFloat(data []byte) (any, error) {
	var f big.Float
	if err := f.GobDecode(data); err != nil {
		return nil, fmt.Errorf("math/big.Float: %w", err)
	}
	return f.Text('f', -1), nil
}

// decodeShopspringDecimal decodes a shopspring/decimal.Decimal binary blob.
//
// Wire format: all bytes except the last 4 are a math/big.Int encoded in the
// same format as decodeBigInt (sign/version byte + big-endian absolute value).
// The last 4 bytes are a big-endian int32 exponent. The decimal value is
// intValue × 10^exp.
func decodeShopspringDecimal(data []byte) (any, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("decimal.Decimal: too short: %d bytes", len(data))
	}
	intData := data[:len(data)-4]
	exp := int32(binary.BigEndian.Uint32(data[len(data)-4:]))

	v, err := decodeBigInt(intData)
	if err != nil {
		return nil, fmt.Errorf("decimal.Decimal: %w", err)
	}
	s := v.(string)

	if s == "0" {
		return "0", nil
	}

	if exp >= 0 {
		return s + strings.Repeat("0", int(exp)), nil
	}

	neg := false
	digits := s
	if len(s) > 0 && s[0] == '-' {
		neg = true
		digits = s[1:]
	}

	e := int(-exp)
	var result string
	if e >= len(digits) {
		result = "0." + strings.Repeat("0", e-len(digits)) + digits
	} else {
		result = digits[:len(digits)-e] + "." + digits[len(digits)-e:]
	}

	if neg {
		return "-" + result, nil
	}
	return result, nil
}

// decodeUUID decodes a UUID BinaryMarshaler blob (16 raw bytes, RFC 4122 layout).
// Applies to github.com/google/uuid and github.com/gofrs/uuid.
func decodeUUID(data []byte) (any, error) {
	if len(data) != 16 {
		return nil, fmt.Errorf("uuid.UUID: expected 16 bytes, got %d", len(data))
	}
	h := hex.EncodeToString(data)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32], nil
}

// bigEndianToDecimal converts a big-endian unsigned integer (as bytes) to a
// decimal string. Uses repeated division by 10.
func bigEndianToDecimal(b []byte) string {
	if len(b) == 0 {
		return "0"
	}
	n := make([]byte, len(b))
	copy(n, b)
	var digits []byte
	for !allZero(n) {
		r := divByTen(n)
		digits = append(digits, '0'+r)
	}
	if len(digits) == 0 {
		return "0"
	}
	// Reverse: digits were collected least-significant-first.
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// divByTen divides the big-endian unsigned integer b by 10 in place.
// Returns the remainder (0–9).
func divByTen(b []byte) byte {
	var r uint16
	for i := range b {
		r = r*256 + uint16(b[i])
		b[i] = byte(r / 10)
		r %= 10
	}
	return byte(r)
}
