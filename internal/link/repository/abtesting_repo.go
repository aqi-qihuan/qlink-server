package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"time"

	"github.com/aqi/qlink-server/internal/link/model"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// ABTestRepo handles AB test data for ds0 (non-sharded).
type ABTestRepo struct {
	db  *gorm.DB
	rdb *redis.Client
}

func NewABTestRepo(db *gorm.DB, rdb *redis.Client) *ABTestRepo {
	return &ABTestRepo{db: db, rdb: rdb}
}

// ——— AB Test CRUD ———

func (r *ABTestRepo) Create(abTest *model.ABTestDO) error {
	return r.db.Create(abTest).Error
}

func (r *ABTestRepo) FindByID(id int64) (*model.ABTestDO, error) {
	var t model.ABTestDO
	err := r.db.Where("id = ? AND del = 0", id).First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *ABTestRepo) FindActiveByShortLinkCode(code string) (*model.ABTestDO, error) {
	var t model.ABTestDO
	err := r.db.Where("short_link_code = ? AND status = ? AND del = 0", code, "running").First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *ABTestRepo) Update(t *model.ABTestDO) error {
	return r.db.Save(t).Error
}

func (r *ABTestRepo) Delete(id int64) error {
	return r.db.Model(&model.ABTestDO{}).Where("id = ?", id).Update("del", 1).Error
}

func (r *ABTestRepo) List(accountNo int64, page, size int, status, linkCode string) ([]model.ABTestDO, int64, error) {
	var list []model.ABTestDO
	var total int64
	query := r.db.Model(&model.ABTestDO{}).Where("account_no = ? AND del = 0", accountNo)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if linkCode != "" {
		query = query.Where("short_link_code = ?", linkCode)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * size
	err := query.Order("id desc").Offset(offset).Limit(size).Find(&list).Error
	return list, total, err
}

// ——— Variant CRUD ———

func (r *ABTestRepo) CreateVariant(v *model.ABTestVariantDO) error {
	return r.db.Create(v).Error
}

func (r *ABTestRepo) FindVariantsByABTestID(abTestID int64) ([]model.ABTestVariantDO, error) {
	var vars []model.ABTestVariantDO
	err := r.db.Where("ab_test_id = ? AND is_active = 1", abTestID).Find(&vars).Error
	return vars, err
}

func (r *ABTestRepo) FindAllVariantsByABTestID(abTestID int64) ([]model.ABTestVariantDO, error) {
	var vars []model.ABTestVariantDO
	err := r.db.Where("ab_test_id = ?", abTestID).Find(&vars).Error
	return vars, err
}

// ——— Redis session stickiness ———

const (
	abTestSessionPrefix = "ab:session:"
	abTestClickPrefix   = "ab:click:"
	sessionTTL          = 24 * time.Hour
)

// SessionID generates a deterministic session ID for a user + AB test.
func SessionID(userIP, userAgent string, abTestID int64) string {
	data := fmt.Sprintf("%s:%s:%d:%s", userIP, userAgent, abTestID, time.Now().Format("2006-01-02"))
	return fmt.Sprintf("ab_se_%x", md5Sum(data))
}

// GetAssignedVariant returns the variant ID this session was assigned to, if any.
func (r *ABTestRepo) GetAssignedVariant(ctx context.Context, sessionID string) (int64, error) {
	if r.rdb == nil {
		return 0, redis.Nil
	}
	key := abTestSessionPrefix + sessionID
	val, err := r.rdb.Get(ctx, key).Int64()
	if err != nil {
		return 0, err
	}
	return val, nil
}

// AssignVariant records which variant was chosen for this session.
func (r *ABTestRepo) AssignVariant(ctx context.Context, sessionID string, variantID int64) error {
	if r.rdb == nil {
		return nil
	}
	key := abTestSessionPrefix + sessionID
	return r.rdb.Set(ctx, key, variantID, sessionTTL).Err()
}

// SelectVariant picks a variant based on weight distribution.
func (r *ABTestRepo) SelectVariant(variants []model.ABTestVariantDO, sessionID string, trafficSplit string) model.ABTestVariantDO {
	if len(variants) == 0 {
		return model.ABTestVariantDO{}
	}
	if len(variants) == 1 {
		return variants[0]
	}

	// Deterministic selection based on session
	seed := int64(0)
	for _, c := range sessionID {
		seed += int64(c)
	}
	rng := rand.New(rand.NewSource(seed))

	switch trafficSplit {
	case "equal":
		return variants[rng.Intn(len(variants))]
	default: // weighted
		randVal := rng.Intn(100)
		cum := 0
		for _, v := range variants {
			cum += v.Weight
			if randVal < cum {
				return v
			}
		}
		return variants[0]
	}
}

// IncrementClickCount increments the click counter for a variant (used for statistics, stored in Redis).
func (r *ABTestRepo) IncrementClickCount(ctx context.Context, abTestID, variantID int64) error {
	if r.rdb == nil {
		return nil
	}
	key := fmt.Sprintf("%s%d:%d", abTestClickPrefix, abTestID, variantID)
	pipe := r.rdb.Pipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, 7*24*time.Hour)
	_, err := pipe.Exec(ctx)
	return err
}

// GetClickCounts returns click counts for all variants of an AB test (from Redis).
func (r *ABTestRepo) GetClickCounts(ctx context.Context, abTestID int64, variantIDs []int64) (map[int64]int64, error) {
	result := make(map[int64]int64)
	if r.rdb == nil {
		return result, nil
	}
	for _, vid := range variantIDs {
		key := fmt.Sprintf("%s%d:%d", abTestClickPrefix, abTestID, vid)
		count, err := r.rdb.Get(ctx, key).Int64()
		if err == nil {
			result[vid] = count
		}
	}
	return result, nil
}

func md5Sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:16])
}
