package service

import (
	"context"
	"errors"
	"time"

	"github.com/aqi/qlink-server/internal/common/response"
	"github.com/aqi/qlink-server/internal/link/model"
	"github.com/aqi/qlink-server/internal/link/repository"
	"github.com/aqi/qlink-server/internal/link/request"
	"github.com/aqi/qlink-server/internal/link/vo"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type ABTestService struct {
	repo *repository.ABTestRepo
	rdb  *redis.Client
}

func NewABTestService(db *gorm.DB, rdb *redis.Client) *ABTestService {
	return &ABTestService{
		repo: repository.NewABTestRepo(db, rdb),
		rdb:  rdb,
	}
}

// Create creates an AB test with variants.
func (s *ABTestService) Create(req *request.CreateABTestRequest, accountNo int64) *response.JsonData {
	// Check no active test for this short link
	existing, _ := s.repo.FindActiveByShortLinkCode(req.ShortLinkCode)
	if existing != nil {
		return response.BuildError("this short link already has a running AB test")
	}

	trafficSplit := req.TrafficSplit
	if trafficSplit == "" {
		trafficSplit = "equal"
	}

	abTest := &model.ABTestDO{
		AccountNo:     accountNo,
		ShortLinkCode: req.ShortLinkCode,
		GroupID:       req.GroupID,
		Name:          req.Name,
		Status:        "draft",
		TrafficSplit:  trafficSplit,
		StartTime:     req.StartTime,
		EndTime:       req.EndTime,
	}
	if err := s.repo.Create(abTest); err != nil {
		return response.BuildError("create ab test failed: " + err.Error())
	}

	// Create variants
	for i, vr := range req.Variants {
		weight := vr.Weight
		if trafficSplit == "equal" {
			weight = 100 / len(req.Variants)
			if i == len(req.Variants)-1 {
				weight = 100 - (weight * (len(req.Variants) - 1))
			}
		}
		isControl := vr.IsControl
		if isControl != 1 {
			isControl = 0
		}

		variant := &model.ABTestVariantDO{
			ABTestID:    abTest.ID,
			Name:        vr.Name,
			TargetURL:   vr.TargetURL,
			Weight:      weight,
			IsControl:   isControl,
			Description: vr.Description,
			IsActive:    1,
		}
		if err := s.repo.CreateVariant(variant); err != nil {
			return response.BuildError("create variant failed: " + err.Error())
		}
	}

	return response.BuildSuccessData(s.toVO(abTest))
}

// Get returns AB test details with variants.
func (s *ABTestService) Get(id int64) *response.JsonData {
	t, err := s.repo.FindByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.BuildError("AB test not found")
		}
		return response.BuildError(err.Error())
	}
	return response.BuildSuccessData(s.toVO(t))
}

// Update updates AB test metadata.
func (s *ABTestService) Update(id int64, req *request.UpdateABTestRequest) *response.JsonData {
	t, err := s.repo.FindByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.BuildError("AB test not found")
		}
		return response.BuildError(err.Error())
	}

	if req.Name != "" {
		t.Name = req.Name
	}
	if req.Description != "" {
		t.Description = req.Description
	}
	if req.TrafficSplit != "" {
		t.TrafficSplit = req.TrafficSplit
	}
	if req.StartTime != nil {
		t.StartTime = req.StartTime
	}
	if req.EndTime != nil {
		t.EndTime = req.EndTime
	}
	if err := s.repo.Update(t); err != nil {
		return response.BuildError(err.Error())
	}
	return response.BuildSuccessData(s.toVO(t))
}

// Start starts a draft AB test.
func (s *ABTestService) Start(id int64, req *request.StartABTestRequest) *response.JsonData {
	t, err := s.repo.FindByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.BuildError("AB test not found")
		}
		return response.BuildError(err.Error())
	}
	if t.Status != "draft" {
		return response.BuildError("only draft AB tests can be started")
	}

	// Check at least 2 active variants
	variants, _ := s.repo.FindVariantsByABTestID(t.ID)
	if len(variants) < 2 {
		return response.BuildError("at least 2 active variants required")
	}

	t.Status = "running"
	if req.StartTime != nil {
		t.StartTime = req.StartTime
	} else {
		now := time.Now()
		t.StartTime = &now
	}
	if err := s.repo.Update(t); err != nil {
		return response.BuildError(err.Error())
	}
	return response.BuildSuccessData(s.toVO(t))
}

// Stop stops a running AB test.
func (s *ABTestService) Stop(id int64, req *request.StopABTestRequest) *response.JsonData {
	t, err := s.repo.FindByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.BuildError("AB test not found")
		}
		return response.BuildError(err.Error())
	}
	if t.Status != "running" {
		return response.BuildError("only running AB tests can be stopped")
	}

	t.Status = "completed"
	if req.EndTime != nil {
		t.EndTime = req.EndTime
	} else {
		now := time.Now()
		t.EndTime = &now
	}
	if err := s.repo.Update(t); err != nil {
		return response.BuildError(err.Error())
	}
	return response.BuildSuccessData(s.toVO(t))
}

// Delete soft-deletes an AB test (draft only).
func (s *ABTestService) Delete(id int64) *response.JsonData {
	t, err := s.repo.FindByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.BuildError("AB test not found")
		}
		return response.BuildError(err.Error())
	}
	if t.Status != "draft" {
		return response.BuildError("only draft AB tests can be deleted")
	}
	if err := s.repo.Delete(id); err != nil {
		return response.BuildError(err.Error())
	}
	return response.BuildSuccess()
}

// List returns paginated AB test list.
func (s *ABTestService) List(req *request.ABTestListRequest, accountNo int64) *response.JsonData {
	page := req.Page
	if page < 1 {
		page = 1
	}
	size := req.Size
	if size < 1 || size > 100 {
		size = 10
	}

	list, total, err := s.repo.List(accountNo, page, size, req.Status, req.ShortLinkCode)
	if err != nil {
		return response.BuildError(err.Error())
	}

	vos := make([]vo.ABTestVO, 0, len(list))
	for _, t := range list {
		vos = append(vos, *s.toVO(&t))
	}

	return response.BuildSuccessData(vo.ABTestListVO{
		Total: total,
		Page:  page,
		Size:  size,
		List:  vos,
	})
}

// Statistics returns click statistics for an AB test.
func (s *ABTestService) Statistics(id int64) *response.JsonData {
	t, err := s.repo.FindByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.BuildError("AB test not found")
		}
		return response.BuildError(err.Error())
	}

	variants, _ := s.repo.FindAllVariantsByABTestID(t.ID)
	ctx := context.Background()

	vids := make([]int64, len(variants))
	for i, v := range variants {
		vids[i] = v.ID
	}
	counts, _ := s.repo.GetClickCounts(ctx, t.ID, vids)

	var totalClicks int64
	variantStats := make([]vo.ABTestVariantStatVO, 0, len(variants))
	var maxClicks int64
	var maxVariant *vo.ABTestVariantVO

	for _, v := range variants {
		c := counts[v.ID]
		totalClicks += c
		stat := vo.ABTestVariantStatVO{
			Variant: vo.ABTestVariantVO{
				ID:          v.ID,
				Name:        v.Name,
				TargetURL:   v.TargetURL,
				Weight:      v.Weight,
				IsControl:   v.IsControl,
				Description: v.Description,
				IsActive:    v.IsActive,
				GmtCreate:   v.GmtCreate,
			},
			ClickCount: c,
		}
		if c > maxClicks {
			maxClicks = c
			maxVariant = &vo.ABTestVariantVO{
				ID:          v.ID,
				Name:        v.Name,
				TargetURL:   v.TargetURL,
				Weight:      v.Weight,
				IsControl:   v.IsControl,
				Description: v.Description,
				IsActive:    v.IsActive,
				GmtCreate:   v.GmtCreate,
			}
		}
		variantStats = append(variantStats, stat)
	}

	// Calculate percentages
	for i := range variantStats {
		if totalClicks > 0 {
			variantStats[i].Percentage = float64(variantStats[i].ClickCount) / float64(totalClicks) * 100
		}
	}

	return response.BuildSuccessData(vo.ABTestStatVO{
		ABTestID:       id,
		Variants:       variantStats,
		WinningVariant: maxVariant,
	})
}

// GetVariantForDispatch resolves which variant to redirect to for the given short link.
// Returns (targetURL, variantID, true) if AB test is active for this link, or ("", 0, false) otherwise.
func (s *ABTestService) GetVariantForDispatch(ctx context.Context, shortLinkCode, userIP, userAgent string) (targetURL string, variantID int64) {
	t, err := s.repo.FindActiveByShortLinkCode(shortLinkCode)
	if err != nil || !t.IsRunning() {
		return "", 0
	}

	variants, err := s.repo.FindVariantsByABTestID(t.ID)
	if err != nil || len(variants) == 0 {
		return "", 0
	}

	// Check Redis for existing session assignment
	sessionID := repository.SessionID(userIP, userAgent, t.ID)
	assignedID, err := s.repo.GetAssignedVariant(ctx, sessionID)
	if err == nil && assignedID > 0 {
		// Verify the assigned variant is still active
		for _, v := range variants {
			if v.ID == assignedID {
				// Record click
				_ = s.repo.IncrementClickCount(ctx, t.ID, v.ID)
				return v.TargetURL, v.ID
			}
		}
	}

	// Select new variant
	selected := s.repo.SelectVariant(variants, sessionID, t.TrafficSplit)
	if selected.ID == 0 {
		return "", 0
	}

	// Persist session assignment
	_ = s.repo.AssignVariant(ctx, sessionID, selected.ID)
	_ = s.repo.IncrementClickCount(ctx, t.ID, selected.ID)

	return selected.TargetURL, selected.ID
}

func (s *ABTestService) toVO(t *model.ABTestDO) *vo.ABTestVO {
	vars, _ := s.repo.FindAllVariantsByABTestID(t.ID)
	variantVOs := make([]vo.ABTestVariantVO, 0, len(vars))
	for _, v := range vars {
		variantVOs = append(variantVOs, vo.ABTestVariantVO{
			ID:          v.ID,
			Name:        v.Name,
			TargetURL:   v.TargetURL,
			Weight:      v.Weight,
			IsControl:   v.IsControl,
			Description: v.Description,
			IsActive:    v.IsActive,
			GmtCreate:   v.GmtCreate,
		})
	}
	return &vo.ABTestVO{
		ID:            t.ID,
		AccountNo:     t.AccountNo,
		ShortLinkCode: t.ShortLinkCode,
		GroupID:       t.GroupID,
		Name:          t.Name,
		Description:   t.Description,
		Status:        t.Status,
		TrafficSplit:  t.TrafficSplit,
		StartTime:     t.StartTime,
		EndTime:       t.EndTime,
		Variants:      variantVOs,
		GmtCreate:     t.GmtCreate,
	}
}
