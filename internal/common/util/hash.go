package util

import (
	"encoding/binary"
	"math/bits"
)

// MurmurHash3Guava computes MurmurHash3 32-bit compatible with Guava's
// Hashing.murmur3_32().hashUnencodedChars(param).padToLong().
// Guava hashes UTF-16 code units (2 bytes per char, little-endian), NOT UTF-8 bytes.
//
// 这里使用纯 Go 实现 x86_32 算法，避免第三方 murmur3 库的 unsafe 指针算术
// 在 go test -race/checkptr 下触发 fatal error。
func MurmurHash3Guava(s string) uint64 {
	utf16le := stringToUTF16LE(s)
	h := murmur3x8632(utf16le, 0)
	return uint64(h) // zero-extend to 64-bit (padToLong)
}

// murmur3x8632 implements MurmurHash3 x86 32-bit with seed.
func murmur3x8632(data []byte, seed uint32) uint32 {
	const (
		c1 = uint32(0xcc9e2d51)
		c2 = uint32(0x1b873593)
	)

	h1 := seed
	roundedEnd := len(data) &^ 3
	for i := 0; i < roundedEnd; i += 4 {
		k1 := binary.LittleEndian.Uint32(data[i : i+4])
		k1 *= c1
		k1 = bits.RotateLeft32(k1, 15)
		k1 *= c2

		h1 ^= k1
		h1 = bits.RotateLeft32(h1, 13)
		h1 = h1*5 + 0xe6546b64
	}

	var k1 uint32
	tail := data[roundedEnd:]
	switch len(tail) {
	case 3:
		k1 ^= uint32(tail[2]) << 16
		fallthrough
	case 2:
		k1 ^= uint32(tail[1]) << 8
		fallthrough
	case 1:
		k1 ^= uint32(tail[0])
		k1 *= c1
		k1 = bits.RotateLeft32(k1, 15)
		k1 *= c2
		h1 ^= k1
	}

	h1 ^= uint32(len(data))
	h1 ^= h1 >> 16
	h1 *= 0x85ebca6b
	h1 ^= h1 >> 13
	h1 *= 0xc2b2ae35
	h1 ^= h1 >> 16
	return h1
}

// stringToUTF16LE converts a Go string (UTF-8) to UTF-16 Little-Endian bytes.
// Each rune is encoded as 2 bytes (BMP) or 4 bytes (surrogate pair for non-BMP).
func stringToUTF16LE(s string) []byte {
	buf := make([]byte, 0, len(s)*2)
	for _, r := range s {
		if r <= 0xFFFF {
			// BMP character: 2 bytes LE
			buf = append(buf, byte(r), byte(r>>8))
		} else {
			// Non-BMP: surrogate pair
			r -= 0x10000
			high := 0xD800 + (r>>10)&0x3FF
			low := 0xDC00 + r&0x3FF
			buf = append(buf, byte(high), byte(high>>8))
			buf = append(buf, byte(low), byte(low>>8))
		}
	}
	return buf
}

// JavaStringHashCode implements Java's String.hashCode() algorithm.
// Used for sharding routing in ShardingDBConfig and ShardingTableConfig.
// Processes UTF-16 code units (same as Java char), including surrogate pairs
// for non-BMP characters (e.g. emoji). Previously iterated over runes and
// masked with 0xFFFF, which dropped the high surrogate and produced
// different hash values than Java for strings containing non-BMP chars.
func JavaStringHashCode(s string) int32 {
	var h int32
	for _, r := range s {
		if r <= 0xFFFF {
			// BMP character: single UTF-16 code unit
			h = 31*h + int32(r)
		} else {
			// Non-BMP: surrogate pair (two UTF-16 code units, matching Java)
			r -= 0x10000
			high := uint16(0xD800 + (r>>10)&0x3FF)
			low := uint16(0xDC00 + r&0x3FF)
			h = 31*h + int32(high)
			h = 31*h + int32(low)
		}
	}
	return h
}

// AbsInt32Mod returns a non-negative modulo result compatible with
// Math.abs(code.hashCode()) % n in Java. Handles the MIN_VALUE edge case.
func AbsInt32Mod(hash int32, n int) int {
	if hash >= 0 {
		return int(hash) % n
	}
	// For negative values, use unsigned interpretation to get absolute value
	return int(uint32(hash) % uint32(n))
}
