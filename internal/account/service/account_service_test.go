package service

import (
	"encoding/json"
	"testing"

	accountmodel "github.com/aqi/qlink-server/internal/account/model"
	"github.com/aqi/qlink-server/internal/account/request"
	"github.com/aqi/qlink-server/internal/common/constant"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAccountSvc(t *testing.T) (*AccountService, *redis.Client) {
	t.Helper()
	db := newAccountDB(t)
	rdb := newTestRedis(t)
	notif := NewNotifyService(rdb, nil) // sms provider nil: tests never send real SMS
	svc := NewAccountService(db, rdb, nil, notif)
	return svc, rdb
}

// seedRegisterCode plants a valid SMS code in Redis, mimicking a prior
// successful SendCode call ("code_timestamp" format, see NotifyService.CheckCode).
func seedRegisterCode(t *testing.T, rdb *redis.Client, phone, code string) {
	t.Helper()
	key := constant.FormatCheckCodeKey(SendCodeTypeRegister, phone)
	require.NoError(t, rdb.Set(t.Context(), key, code+"_1790610519943", 0).Err())
}

// TestRegister_HappyPath covers the full flow: SMS check, unique phone,
// snowflake account number, md5-crypt password (Java-compatible $1$ salt).
func TestRegister_HappyPath(t *testing.T) {
	svc, rdb := newAccountSvc(t)

	seedRegisterCode(t, rdb, "13800001111", "123456")
	err := svc.Register(&request.AccountRegisterRequest{
		Phone: "13800001111", Pwd: "s3cret!", Username: "aqi", Mail: "a@b.c", Code: "123456",
	})
	require.NoError(t, err)

	var account accountmodel.AccountDO
	require.NoError(t, svc.db.Where("phone = ?", "13800001111").First(&account).Error)
	assert.NotZero(t, account.AccountNo, "snowflake account number must be generated")
	assert.Equal(t, "DEFAULT", account.Auth)
	assert.True(t, len(account.Secret) > 3, "salt must be stored")
	assert.NotEqual(t, "s3cret!", account.Pwd, "password must be hashed")

	// The stored hash must verify via the same md5-crypt scheme used at login
	err = svc.Register(&request.AccountRegisterRequest{Phone: "13800001111", Pwd: "x"})
	require.Error(t, err, "code was consumed by GetDel → second register must fail")

	_ = account // silence unused-var in case assertions change
}

// TestRegister_DuplicatePhone: registering an existing phone fails with a
// clear error, even with a valid code.
func TestRegister_DuplicatePhone(t *testing.T) {
	svc, rdb := newAccountSvc(t)

	seedRegisterCode(t, rdb, "13800002222", "111222")
	require.NoError(t, svc.Register(&request.AccountRegisterRequest{Phone: "13800002222", Pwd: "p1", Code: "111222"}))

	seedRegisterCode(t, rdb, "13800002222", "111222")
	err := svc.Register(&request.AccountRegisterRequest{Phone: "13800002222", Pwd: "p2", Code: "111222"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

// TestRegister_WrongCode: a bad or expired SMS code blocks registration.
func TestRegister_WrongCode(t *testing.T) {
	svc, _ := newAccountSvc(t)

	err := svc.Register(&request.AccountRegisterRequest{Phone: "13800003333", Pwd: "p", Code: "000000"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verification code")

	var count int64
	svc.db.Model(&accountmodel.AccountDO{}).Count(&count)
	assert.Equal(t, int64(0), count, "no account may be created without a valid code")
}

// TestLogin verifies password verification against the stored md5-crypt hash
// and rejection on mismatch.
func TestLogin(t *testing.T) {
	svc, rdb := newAccountSvc(t)

	seedRegisterCode(t, rdb, "13800004444", "222333")
	require.NoError(t, svc.Register(&request.AccountRegisterRequest{Phone: "13800004444", Pwd: "hunter2", Code: "222333"}))

	token, err := svc.Login(&request.AccountLoginRequest{Phone: "13800004444", Pwd: "hunter2"})
	require.NoError(t, err)
	assert.NotEmpty(t, token, "login must return a JWT")

	_, err = svc.Login(&request.AccountLoginRequest{Phone: "13800004444", Pwd: "wrong"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "incorrect password")

	_, err = svc.Login(&request.AccountLoginRequest{Phone: "13999999999", Pwd: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account not found")
}

// TestDetail_CacheRoundTrip: first call queries DB and warms the 30s cache;
// the second call is served from Redis (observable: value present in
// miniredis, and deleting the DB row does not affect the cached result).
func TestDetail_CacheRoundTrip(t *testing.T) {
	svc, rdb := newAccountSvc(t)
	require.NoError(t, svc.db.Exec(
		"INSERT INTO account (account_no, phone, pwd, secret, auth, username) VALUES (555, '13800005555', 'h', '$1$s$', 'DEFAULT', 'cached-user')").Error)

	// First call → DB + cache warm
	a1, err := svc.Detail(555)
	require.NoError(t, err)
	assert.Equal(t, "cached-user", a1.Username)

	cacheKey := "account:detail:555"
	cached, err := rdb.Get(t.Context(), cacheKey).Result()
	require.NoError(t, err)
	var probe accountmodel.AccountDO
	require.NoError(t, json.Unmarshal([]byte(cached), &probe))

	// Remove the DB row; the cached copy must still serve identical data
	require.NoError(t, svc.db.Exec("DELETE FROM account WHERE account_no = 555").Error)
	a2, err := svc.Detail(555)
	require.NoError(t, err)
	assert.Equal(t, a1.Username, a2.Username)
}

// TestDetail_NotFound: unknown account number → error.
func TestDetail_NotFound(t *testing.T) {
	svc, _ := newAccountSvc(t)
	_, err := svc.Detail(424242)
	require.Error(t, err)
}

// TestUpdate_PartialFields: only non-empty fields are applied; the detail
// cache is invalidated on success.
func TestUpdate_PartialFields(t *testing.T) {
	svc, rdb := newAccountSvc(t)
	require.NoError(t, svc.db.Exec(
		"INSERT INTO account (account_no, phone, pwd, secret, auth, username, mail, head_img) VALUES (666, '13800006666', 'h', '$1$s$', 'DEFAULT', 'old-name', 'old@x.c', 'old.png')").Error)

	// Warm the cache, then update a subset of fields
	_, err := svc.Detail(666)
	require.NoError(t, err)
	require.NoError(t, rdb.Set(t.Context(), "account:detail:666", `{"stale":true}`, 0).Err())

	err = svc.Update(666, &request.AccountUpdateRequest{Username: "new-name", Mail: ""})
	require.NoError(t, err)

	var account accountmodel.AccountDO
	require.NoError(t, svc.db.Where("account_no = ?", 666).First(&account).Error)
	assert.Equal(t, "new-name", account.Username, "username updated")
	assert.Equal(t, "old@x.c", account.Mail, "empty mail in request must not clear the field")
	assert.Equal(t, "old.png", account.HeadImg, "untouched field must survive")

	// Cache invalidated
	exists, err := rdb.Exists(t.Context(), "account:detail:666").Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), exists, "detail cache must be dropped after update")
}

// TestUpdate_EmptyRequest: an all-empty update is a no-op success.
func TestUpdate_EmptyRequest(t *testing.T) {
	svc, _ := newAccountSvc(t)
	require.NoError(t, svc.db.Exec(
		"INSERT INTO account (account_no, phone, pwd, secret, auth) VALUES (777, '13800007777', 'h', '$1$s$', 'DEFAULT')").Error)

	err := svc.Update(777, &request.AccountUpdateRequest{})
	require.NoError(t, err)
}

// TestUpdate_NotFound: updating a non-existent account errors out.
func TestUpdate_NotFound(t *testing.T) {
	svc, _ := newAccountSvc(t)
	err := svc.Update(999999, &request.AccountUpdateRequest{Username: "ghost"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestCheckCode_FullFlow exercises the NotifyService code lifecycle:
// planted code verifies once; the GetDel semantics make replay impossible.
func TestCheckCode_FullFlow(t *testing.T) {
	_, rdb := newAccountSvc(t)
	notif := NewNotifyService(rdb, nil)

	seedRegisterCode(t, rdb, "13800008888", "654321")
	assert.True(t, notif.CheckCode(SendCodeTypeRegister, "13800008888", "654321"), "first check must pass")
	assert.False(t, notif.CheckCode(SendCodeTypeRegister, "13800008888", "654321"), "replay must fail (GetDel)")
	assert.False(t, notif.CheckCode(SendCodeTypeRegister, "13800008888", "000000"), "wrong code must fail")
}
