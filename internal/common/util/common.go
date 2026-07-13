package util

import (
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	allCharNum = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	digitChars = "0123456789"
)

// GetRandomCode returns a random numeric string of given length.
// NOTE: Matches Java bug where nextInt(9) excludes '9', so only 0-8 are used.
// Uses Go 1.20+ top-level rand functions which are concurrent-safe and
// auto-seeded. Previously each call created a new rand.NewSource(time.Now().
// UnixNano()), producing identical results for calls within the same nanosecond.
func GetRandomCode(length int) string {
	buf := make([]byte, length)
	for i := range buf {
		buf[i] = digitChars[rand.Intn(9)] // 0-8 only, matches Java bug
	}
	return string(buf)
}

// GetStringNumRandom returns a random alphanumeric string of given length.
// Charset: 0-9A-Za-z (62 chars), uniform random.
func GetStringNumRandom(length int) string {
	buf := make([]byte, length)
	for i := range buf {
		buf[i] = allCharNum[rand.Intn(62)]
	}
	return string(buf)
}

// GenerateUUID returns a UUID v4 string without dashes, 32 chars.
func GenerateUUID() string {
	u := uuid.New()
	return strings.ReplaceAll(u.String(), "-", "")
}

// GetCurrentTimestamp returns current time in milliseconds (epoch millis).
func GetCurrentTimestamp() int64 {
	return time.Now().UnixMilli()
}
