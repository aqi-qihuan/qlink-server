package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// ========== #6 限流器并发安全（mutex 替代有缺陷的 lock-free）==========

func TestRateBucket_AllowRespectsBurst(t *testing.T) {
	b := newRateBucket(1.0, 1) // burst=1 → 初始 1000 tokens
	assert.True(t, b.allow(), "第 1 次应放行")
	assert.False(t, b.allow(), "burst 耗尽后应拒绝（无 refill 时）")
}

func TestRateBucket_AllowRespectsBurstMultiple(t *testing.T) {
	burst := 5
	b := newRateBucket(10.0, burst)
	allowed := 0
	for i := 0; i < burst+3; i++ {
		if b.allow() {
			allowed++
		}
	}
	assert.Equal(t, burst, allowed, "初始只应放行 burst 次")
}

func TestRateBucket_ConcurrentNoPanic(t *testing.T) {
	b := newRateBucket(100.0, 2)
	const N = 200
	var wg sync.WaitGroup
	allowed := int64(0)
	var mu sync.Mutex
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			if b.allow() {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	// 瞬时（elapsed≈0，无 refill）放行的数量不应超过 burst
	assert.LessOrEqual(t, int(allowed), 2, "并发瞬时放行不应超过 burst（修复前 lock-free 会按并发数放大 refill）")
}

// ========== #38 RPC token 常量时间比较 ==========

func TestValidateRPCToken_DefaultRejected(t *testing.T) {
	t.Setenv("RPC_TOKEN", "rpc-token-default")
	err := ValidateRPCToken()
	assert.Error(t, err, "使用默认 rpc-token 应报错（强制要求安全值）")
}

func TestValidateRPCToken_SecureAccepted(t *testing.T) {
	t.Setenv("RPC_TOKEN", "a-secure-random-token-9f3c")
	err := ValidateRPCToken()
	assert.NoError(t, err, "配置了安全 rpc-token 应通过校验")
}

func TestRpcTokenMiddleware_AcceptAndReject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const expected = "rpc-token-secret-xyz"

	ok := false
	r := gin.New()
	r.Use(RpcTokenMiddleware(expected))
	r.GET("/protected", func(c *gin.Context) {
		ok = true
		c.Status(http.StatusOK)
	})

	// 有效 token → 通过
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("rpc-token", expected)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.True(t, ok, "有效 rpc-token 应放行到 handler")
	assert.Equal(t, http.StatusOK, w.Code)

	// 无效 token → 拒绝（且不应进入 handler）
	ok = false
	req2 := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req2.Header.Set("rpc-token", "wrong-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.False(t, ok, "无效 rpc-token 不应进入 handler")
}
