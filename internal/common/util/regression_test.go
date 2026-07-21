package util

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ========== #34 随机数播种/并发安全 ==========

func TestGetRandomCode_CharsetAndLength(t *testing.T) {
	for _, length := range []int{1, 4, 6, 11, 32} {
		code := GetRandomCode(length)
		assert.Equal(t, length, len(code), "长度应等于入参")
		for _, c := range code {
			// digitChars = "0123456789" 但按 Java 兼容只取 0-8
			assert.GreaterOrEqual(t, int(c), int('0'), "字符应在 0-9 范围")
			assert.LessOrEqual(t, int(c), int('8'), "字符应在 0-8 范围（兼容 Java nextInt(9)）")
		}
	}
}

func TestGetStringNumRandom_CharsetAndLength(t *testing.T) {
	const all = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	for _, length := range []int{1, 8, 16, 64} {
		s := GetStringNumRandom(length)
		assert.Equal(t, length, len(s), "长度应等于入参")
		for _, c := range s {
			assert.Contains(t, all, string(c), "字符应在 0-9A-Za-z 范围")
		}
	}
}

func TestRandom_ConcurrentNoPanic(t *testing.T) {
	// 1000 个 goroutine 并发调用，验证无 data race / panic
	const N = 1000
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			_ = GetRandomCode(6)
			_ = GetStringNumRandom(10)
		}()
	}
	wg.Wait()
}

func TestRandom_DifferentResults(t *testing.T) {
	// 同一纳秒内多次调用应产生不同结果（修复前用 time.Now().UnixNano() 播种会重复）
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		seen[GetStringNumRandom(12)] = true
	}
	assert.Greater(t, len(seen), 90, "100 次调用应几乎全部不同（修复前会大量重复）")
}

// ========== #4 Snowflake 不返回 0 ==========

func TestGetMachineID_ValidRange(t *testing.T) {
	// getMachineID 应返回 [0, 1024) 的合法 machine id
	id, err := getMachineID()
	assert.NoError(t, err, "getMachineID 不应返回错误")
	assert.LessOrEqual(t, int(id), 1023, "machineID 应在 [0,1024) 范围")
}
