package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ========== #3 md5crypt 失败返回 error（修复前返回空串导致任意密码可登录）==========

func TestMd5CryptHash_ValidInput(t *testing.T) {
	salt := "$1$" + "AbCdEfGh" // 合法 $1$ + 8 字符 salt
	hash, err := md5CryptHash("Test123456", salt)
	assert.NoError(t, err, "合法输入不应报错")
	assert.NotEmpty(t, hash, "成功时哈希不应为空（修复前失败会返回空串）")
	assert.True(t, strings.HasPrefix(hash, "$1$"), "md5-crypt 哈希应以 $1$ 开头")
}

func TestMd5CryptHash_InvalidSaltReturnsError(t *testing.T) {
	// 非法 salt 应返回 error（而非空串），上层据此拒绝登录
	cases := []string{
		"",          // 空
		"not-a-salt", // 不含 $1$ 前缀
		"$$$",        // 格式错误
	}
	for _, salt := range cases {
		hash, err := md5CryptHash("whatever", salt)
		assert.Error(t, err, "非法 salt 应返回 error: %q", salt)
		assert.Empty(t, hash, "出错时哈希应为空串")
	}
}

func TestMd5CryptHash_Deterministic(t *testing.T) {
	salt := "$1$" + "XyZ12345"
	h1, err1 := md5CryptHash("MyPassword", salt)
	h2, err2 := md5CryptHash("MyPassword", salt)
	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Equal(t, h1, h2, "相同密码+salt 应产生相同哈希（登录校验依赖此特性）")
}

func TestMd5CryptHash_DifferentPasswordDifferentHash(t *testing.T) {
	salt := "$1$" + "QwErTy12"
	h1, _ := md5CryptHash("PasswordA", salt)
	h2, _ := md5CryptHash("PasswordB", salt)
	assert.NotEqual(t, h1, h2, "不同密码应产生不同哈希")
}
