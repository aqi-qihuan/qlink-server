package service

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	accountmodel "github.com/aqi/qlink-server/internal/account/model"
	"github.com/aqi/qlink-server/internal/account/request"
	"github.com/aqi/qlink-server/internal/common/constant"
	"github.com/aqi/qlink-server/internal/common/model"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTrafficSvc(t *testing.T) (*TrafficService, []*redis.Client) {
	t.Helper()
	dbs := newTrafficDBs(t)
	rdb := newTestRedis(t)
	accountDB := newAccountDB(t)
	svc := NewTrafficService(dbs, rdb, nil, "", accountDB)
	return svc, nil
}

// TestGetTrafficSharding locks the account_no%2 routing: even → traffic_0,
// odd → traffic_1.
func TestGetTrafficSharding(t *testing.T) {
	cases := []struct {
		accountNo int64
		table     string
	}{
		{13900139000, "traffic_0"},
		{13900139001, "traffic_1"},
		{13900139003, "traffic_1"}, // the account from the a11MpqOa incident
		{2, "traffic_0"},
	}
	for _, c := range cases {
		if got := getTrafficTableName(c.accountNo); got != c.table {
			t.Errorf("accountNo=%d → table %s, want %s", c.accountNo, got, c.table)
		}
	}
}

// TestReduce_HappyPath: a pack with remaining quota → day_used+1, a LOCK
// task row is created, and the Redis remaining cache is set.
func TestReduce_HappyPath(t *testing.T) {
	svc, _ := newTrafficSvc(t)
	const accountNo = int64(100)
	db := svc.dbs[0]

	seedPack(t, db, "traffic_0", packOf(accountNo, 10, 2, tomorrow()))

	err := svc.Reduce(&request.UseTrafficRequest{AccountNo: accountNo, BizID: "a11MpqOa"})
	require.NoError(t, err)

	// day_used incremented on the pack
	var pack accountmodel.TrafficDO
	require.NoError(t, db.Table("traffic_0").Where("account_no = ?", accountNo).First(&pack).Error)
	assert.Equal(t, 3, pack.DayUsed)

	// traffic task recorded with LOCK state and biz id
	var task accountmodel.TrafficTaskDO
	require.NoError(t, db.Where("account_no = ?", accountNo).First(&task).Error)
	assert.Equal(t, pack.ID, task.TrafficID)
	assert.Equal(t, "LOCK", task.LockState)
	assert.Equal(t, "a11MpqOa", task.BizID)
	assert.Equal(t, 1, task.UseTimes)

	// Redis remaining = total remaining (10-2) - 1 = 7
	remain, err := svc.rdb.Get(t.Context(), constant.FormatDayTotalTrafficKey(accountNo)).Int()
	require.NoError(t, err)
	assert.Equal(t, 7, remain)
}

// TestReduce_DayUsedResetYesterday: a pack last modified yesterday (stale
// day_used from the previous day) must be reset to 0 before the quota
// calculation — otherwise yesterday's usage permanently eats today's quota.
func TestReduce_DayUsedResetYesterday(t *testing.T) {
	svc, _ := newTrafficSvc(t)
	const accountNo = int64(100)
	db := svc.dbs[0]

	p := packOf(accountNo, 10, 10, tomorrow()) // exhausted yesterday
	p.GmtModified = yesterday()
	seedPack(t, db, "traffic_0", p)

	err := svc.Reduce(&request.UseTrafficRequest{AccountNo: accountNo})
	require.NoError(t, err)

	// day_used was reset to 0, then incremented to 1
	var pack accountmodel.TrafficDO
	require.NoError(t, db.Table("traffic_0").Where("account_no = ?", accountNo).First(&pack).Error)
	assert.Equal(t, 1, pack.DayUsed)

	// remaining = full fresh quota 10 - 1 = 9 (not negative)
	remain, err := svc.rdb.Get(t.Context(), constant.FormatDayTotalTrafficKey(accountNo)).Int()
	require.NoError(t, err)
	assert.Equal(t, 9, remain)
}

// TestReduce_Exhausted: all packs drained today → "traffic exhausted".
// This is exactly the failure that left short link a11MpqOa unwritten in
// the 2026-09-28 incident (reduceTraffic failed → MQ 落库放弃).
func TestReduce_Exhausted(t *testing.T) {
	svc, _ := newTrafficSvc(t)
	const accountNo = int64(100)
	db := svc.dbs[0]

	seedPack(t, db, "traffic_0", packOf(accountNo, 10, 10, tomorrow())) // full today

	err := svc.Reduce(&request.UseTrafficRequest{AccountNo: accountNo})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "traffic exhausted")

	// No task row was written
	var count int64
	db.Model(&accountmodel.TrafficTaskDO{}).Count(&count)
	assert.Equal(t, int64(0), count)
}

// TestReduce_ExpiredPackIgnored: an expired pack is not returned by the
// active-pack query → "no available traffic pack".
func TestReduce_ExpiredPackIgnored(t *testing.T) {
	svc, _ := newTrafficSvc(t)
	const accountNo = int64(100)
	db := svc.dbs[0]

	seedPack(t, db, "traffic_0", packOf(accountNo, 10, 0, yesterday())) // expired

	err := svc.Reduce(&request.UseTrafficRequest{AccountNo: accountNo})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no available traffic pack")
}

// TestReduce_MultiPackSpillover: when the first pack is drained, the
// deduction spills over to the second pack.
func TestReduce_MultiPackSpillover(t *testing.T) {
	svc, _ := newTrafficSvc(t)
	const accountNo = int64(100)
	db := svc.dbs[0]

	first := seedPack(t, db, "traffic_0", packOf(accountNo, 5, 5, tomorrow()))  // full
	second := seedPack(t, db, "traffic_0", packOf(accountNo, 20, 0, tomorrow())) // fresh

	err := svc.Reduce(&request.UseTrafficRequest{AccountNo: accountNo})
	require.NoError(t, err)

	var p1, p2 accountmodel.TrafficDO
	require.NoError(t, db.Table("traffic_0").Where("id = ?", first.ID).First(&p1).Error)
	require.NoError(t, db.Table("traffic_0").Where("id = ?", second.ID).First(&p2).Error)
	assert.Equal(t, 5, p1.DayUsed, "full pack must stay untouched")
	assert.Equal(t, 1, p2.DayUsed, "spillover must hit the second pack")

	// remaining across packs = (5-5) + (20-0) - 1 = 19
	remain, err := svc.rdb.Get(t.Context(), constant.FormatDayTotalTrafficKey(accountNo)).Int()
	require.NoError(t, err)
	assert.Equal(t, 19, remain)
}

// TestHandleTrafficUsed_Rollback: a task still in LOCK (short link creation
// failed) → day_used decremented, task → CANCEL, Redis remaining +1.
func TestHandleTrafficUsed_Rollback(t *testing.T) {
	svc, _ := newTrafficSvc(t)
	const accountNo = int64(100)
	db := svc.dbs[0]

	pack := seedPack(t, db, "traffic_0", packOf(accountNo, 10, 4, tomorrow()))
	task := accountmodel.TrafficTaskDO{
		AccountNo: accountNo, TrafficID: pack.ID, UseTimes: 1, LockState: "LOCK", BizID: "code-x",
	}
	require.NoError(t, db.Create(&task).Error)

	// Seed the remaining cache so the Incr(+1) is observable
	remainKey := constant.FormatDayTotalTrafficKey(accountNo)
	require.NoError(t, svc.rdb.Set(t.Context(), remainKey, 5, time.Hour).Err())

	err := svc.HandleTrafficMessage(&model.EventMessage{
		EventMessageType: "TRAFFIC_USED",
		BizId:            fmt.Sprintf("%d", task.ID),
		AccountNo:        accountNo,
	})
	require.NoError(t, err)

	var after accountmodel.TrafficDO
	require.NoError(t, db.Table("traffic_0").Where("id = ?", pack.ID).First(&after).Error)
	assert.Equal(t, 3, after.DayUsed, "rollback must decrement day_used")

	var afterTask accountmodel.TrafficTaskDO
	require.NoError(t, db.Where("id = ?", task.ID).First(&afterTask).Error)
	assert.Equal(t, "CANCEL", afterTask.LockState)

	remain, err := svc.rdb.Get(t.Context(), remainKey).Int()
	require.NoError(t, err)
	assert.Equal(t, 6, remain, "Redis remaining must be restored via Incr")
}

// TestHandleTrafficUsed_NoDoubleRollback: an already-CANCEL task must not
// roll back again (the delayed message is redelivered — this is the
// multi-return protection).
func TestHandleTrafficUsed_NoDoubleRollback(t *testing.T) {
	svc, _ := newTrafficSvc(t)
	const accountNo = int64(100)
	db := svc.dbs[0]

	pack := seedPack(t, db, "traffic_0", packOf(accountNo, 10, 4, tomorrow()))
	task := accountmodel.TrafficTaskDO{
		AccountNo: accountNo, TrafficID: pack.ID, UseTimes: 1, LockState: "CANCEL", BizID: "code-x",
	}
	require.NoError(t, db.Create(&task).Error)

	err := svc.HandleTrafficMessage(&model.EventMessage{
		EventMessageType: "TRAFFIC_USED",
		BizId:            fmt.Sprintf("%d", task.ID),
		AccountNo:        accountNo,
	})
	require.NoError(t, err, "redelivery on CANCEL must be a silent no-op")

	var after accountmodel.TrafficDO
	require.NoError(t, db.Table("traffic_0").Where("id = ?", pack.ID).First(&after).Error)
	assert.Equal(t, 4, after.DayUsed, "CANCEL task must not decrement again")
}

// TestHandleOrderPay: a paid order creates a pack sized by product×buyNum
// and upgrades the account auth level for SECOND/THIRD products.
func TestHandleOrderPay(t *testing.T) {
	svc, _ := newTrafficSvc(t)
	const accountNo = int64(101) // odd → traffic_1

	// account exists with DEFAULT auth
	require.NoError(t, svc.accountDB.Exec(
		"INSERT INTO account (account_no, phone, pwd, secret, auth) VALUES (?, ?, ?, ?, ?)",
		accountNo, "13800000001", "hash", "$1$salt$", "DEFAULT").Error)

	content, _ := json.Marshal(map[string]interface{}{
		"outTradeNo": "ORDER-1",
		"buyNum":     2,
		"product":    map[string]interface{}{"dayTimes": 50, "validDay": 30, "level": "SECOND", "id": 7},
	})

	err := svc.HandleTrafficMessage(&model.EventMessage{
		EventMessageType: "PRODUCT_ORDER_PAY",
		AccountNo:        accountNo,
		Content:          string(content),
	})
	require.NoError(t, err)

	// Pack created on the odd shard with day_limit = 50*2
	var pack accountmodel.TrafficDO
	require.NoError(t, svc.dbs[1].Table("traffic_1").Where("account_no = ?", accountNo).First(&pack).Error)
	assert.Equal(t, 100, pack.DayLimit)
	assert.Equal(t, 0, pack.DayUsed)
	assert.Equal(t, "ORDER-1", pack.OutTradeNo)

	// Auth upgraded DEFAULT → GOLD (SECOND product)
	var auth string
	require.NoError(t, svc.accountDB.Raw("SELECT auth FROM account WHERE account_no = ?", accountNo).Scan(&auth).Error)
	assert.Equal(t, "GOLD", auth)
}

// TestHandleFreeInit: registration event creates the 10/day free pack that
// expires today.
func TestHandleFreeInit(t *testing.T) {
	svc, _ := newTrafficSvc(t)
	const accountNo = int64(100)

	err := svc.HandleTrafficMessage(&model.EventMessage{
		EventMessageType: "TRAFFIC_FREE_INIT",
		AccountNo:        accountNo,
	})
	require.NoError(t, err)

	var pack accountmodel.TrafficDO
	require.NoError(t, svc.dbs[0].Table("traffic_0").Where("account_no = ?", accountNo).First(&pack).Error)
	assert.Equal(t, 10, pack.DayLimit)
	assert.Equal(t, "free_init", pack.OutTradeNo)
	assert.Equal(t, "FIRST", pack.Level)

	// Free pack expires today → not usable tomorrow
	expiry := pack.ExpiredDate
	require.NotNil(t, expiry)
	assert.WithinDuration(t, time.Now(), *expiry, time.Minute)
}

// TestClaimFree_IdempotentPerDay: the Redis SETNX lock allows only one
// claim per account per day; the shop service is called at most once.
func TestClaimFree_IdempotentPerDay(t *testing.T) {
	dbs := newTrafficDBs(t)
	rdb := newTestRedis(t)

	shop := newShopServer(t, 200, `{"code":0,"data":{"id":1,"level":"FIRST","dayTimes":10,"totalTimes":100,"validDay":1,"pluginType":"short_link"}}`)
	svc := NewTrafficService(dbs, rdb, nil, shop.URL, nil)

	const accountNo = int64(202)
	require.NoError(t, svc.ClaimFree(accountNo))

	// First claim created the pack
	var pack accountmodel.TrafficDO
	require.NoError(t, dbs[0].Table("traffic_0").Where("account_no = ?", accountNo).First(&pack).Error)
	assert.Equal(t, 10, pack.DayLimit)

	// Same day second claim → rejected
	err := svc.ClaimFree(accountNo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "今日已领取")

	// No duplicate pack
	var count int64
	dbs[0].Table("traffic_0").Where("account_no = ?", accountNo).Count(&count)
	assert.Equal(t, int64(1), count)
}

// TestClaimFree_ShopDownRollsBackLock: when the shop service is
// unavailable, the idempotency lock must be released so the user can retry.
func TestClaimFree_ShopDownRollsBackLock(t *testing.T) {
	dbs := newTrafficDBs(t)
	rdb := newTestRedis(t)

	shop := newShopServer(t, 500, `{"code":500}`)
	svc := NewTrafficService(dbs, rdb, nil, shop.URL, nil)

	const accountNo = int64(303)
	err := svc.ClaimFree(accountNo)
	require.Error(t, err)

	// The claim lock must have been rolled back (shop failed → product fetch fails)
	err = svc.ClaimFree(accountNo)
	require.Error(t, err, "second attempt should also fail but must not be blocked by a stale lock")
	// Distinguish failure causes: both fail, but the second call must be a
	// fresh shop attempt, not "今日已领取" (which would indicate a leaked lock).
	assert.NotContains(t, err.Error(), "今日已领取")
}

// TestDeleteExpireTraffic: the daily job removes expired packs from both shards.
func TestDeleteExpireTraffic(t *testing.T) {
	svc, _ := newTrafficSvc(t)

	seedPack(t, svc.dbs[0], "traffic_0", packOf(1, 10, 0, yesterday()))  // expired
	seedPack(t, svc.dbs[0], "traffic_0", packOf(2, 10, 0, tomorrow()))   // active
	seedPack(t, svc.dbs[1], "traffic_1", packOf(3, 10, 0, yesterday()))  // expired (odd shard)

	svc.DeleteExpireTraffic()

	var n0, n1, active int64
	svc.dbs[0].Table("traffic_0").Where("account_no = 1").Count(&n0)
	svc.dbs[0].Table("traffic_0").Where("account_no = 2").Count(&active)
	svc.dbs[1].Table("traffic_1").Where("account_no = 3").Count(&n1)
	assert.Equal(t, int64(0), n0, "expired pack on shard 0 must be deleted")
	assert.Equal(t, int64(1), active, "active pack must survive")
	assert.Equal(t, int64(0), n1, "expired pack on shard 1 must be deleted")
}
