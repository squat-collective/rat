package reaper

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Mock stores ─────────────────────────────────────────────────

type mockSettingsStore struct {
	mu       sync.Mutex
	settings map[string]json.RawMessage
	status   *domain.ReaperStatus
}

func newMockSettingsStore(cfg domain.RetentionConfig) *mockSettingsStore {
	data, _ := json.Marshal(cfg)
	return &mockSettingsStore{
		settings: map[string]json.RawMessage{"retention": data},
		status:   &domain.ReaperStatus{},
	}
}

func (m *mockSettingsStore) GetSetting(_ context.Context, key string) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.settings[key]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return v, nil
}

func (m *mockSettingsStore) PutSetting(_ context.Context, key string, value json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings[key] = value
	return nil
}

func (m *mockSettingsStore) GetReaperStatus(_ context.Context) (*domain.ReaperStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status, nil
}

func (m *mockSettingsStore) UpdateReaperStatus(_ context.Context, s *domain.ReaperStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = s
	return nil
}

type mockRunStore struct {
	mu   sync.Mutex
	runs []domain.Run
	// Track calls
	deletedBeyondLimit map[uuid.UUID]int
	deletedOlderThan   int
}

func newMockRunStore() *mockRunStore {
	return &mockRunStore{deletedBeyondLimit: make(map[uuid.UUID]int)}
}

func (m *mockRunStore) ListRuns(_ context.Context, _ api.RunFilter) ([]domain.Run, error) {
	return m.runs, nil
}

func (m *mockRunStore) CountRuns(_ context.Context, _ api.RunFilter) (int, error) {
	return len(m.runs), nil
}

func (m *mockRunStore) GetRun(_ context.Context, runID string) (*domain.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, _ := uuid.Parse(runID)
	for i := range m.runs {
		if m.runs[i].ID == id {
			return &m.runs[i], nil
		}
	}
	return nil, nil
}
func (m *mockRunStore) CreateRun(_ context.Context, _ *domain.Run) error { return nil }
func (m *mockRunStore) UpdateRunStatus(_ context.Context, runID string, status domain.RunStatus, errMsg *string, _ *int64, _ *int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, _ := uuid.Parse(runID)
	for i := range m.runs {
		if m.runs[i].ID == id {
			m.runs[i].Status = status
			if errMsg != nil {
				m.runs[i].Error = errMsg
			}
		}
	}
	return nil
}
func (m *mockRunStore) GetRunLogs(_ context.Context, _ string) ([]api.LogEntry, error) {
	return nil, nil
}
func (m *mockRunStore) SaveRunLogs(_ context.Context, _ string, _ []api.LogEntry) error {
	return nil
}
func (m *mockRunStore) DeleteRunsBeyondLimit(_ context.Context, pipelineID uuid.UUID, keepCount int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedBeyondLimit[pipelineID] = keepCount
	return 5, nil // pretend we deleted 5
}
func (m *mockRunStore) DeleteRunsOlderThan(_ context.Context, _ time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedOlderThan = 3
	return 3, nil
}
func (m *mockRunStore) ListStuckRuns(_ context.Context, cutoff time.Time) ([]domain.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var stuck []domain.Run
	for _, r := range m.runs {
		if (r.Status == domain.RunStatusPending || r.Status == domain.RunStatusRunning) && r.CreatedAt.Before(cutoff) {
			stuck = append(stuck, r)
		}
	}
	return stuck, nil
}

func (m *mockRunStore) LatestRunPerPipeline(_ context.Context, _ []uuid.UUID) (map[uuid.UUID]*domain.Run, error) {
	return nil, nil
}

type mockPipelineStore struct {
	mu             sync.Mutex
	pipelines      []domain.Pipeline
	softDeleted    []domain.Pipeline
	hardDeleted    []uuid.UUID
	retentionCalls map[uuid.UUID]json.RawMessage
}

func newMockPipelineStore() *mockPipelineStore {
	return &mockPipelineStore{retentionCalls: make(map[uuid.UUID]json.RawMessage)}
}

func (m *mockPipelineStore) ListPipelines(_ context.Context, _ api.PipelineFilter) ([]domain.Pipeline, error) {
	return m.pipelines, nil
}

func (m *mockPipelineStore) CountPipelines(_ context.Context, _ api.PipelineFilter) (int, error) {
	return len(m.pipelines), nil
}

func (m *mockPipelineStore) GetPipeline(_ context.Context, ns, layer, name string) (*domain.Pipeline, error) {
	for i := range m.pipelines {
		if m.pipelines[i].Namespace == ns && string(m.pipelines[i].Layer) == layer && m.pipelines[i].Name == name {
			return &m.pipelines[i], nil
		}
	}
	return nil, nil
}
func (m *mockPipelineStore) GetPipelineByID(_ context.Context, id string) (*domain.Pipeline, error) {
	return nil, nil
}
func (m *mockPipelineStore) CreatePipeline(_ context.Context, _ *domain.Pipeline) error { return nil }
func (m *mockPipelineStore) UpdatePipeline(_ context.Context, _, _, _ string, _ api.UpdatePipelineRequest) (*domain.Pipeline, error) {
	return nil, nil
}
func (m *mockPipelineStore) DeletePipeline(_ context.Context, _, _, _ string) error { return nil }
func (m *mockPipelineStore) SetDraftDirty(_ context.Context, _, _, _ string, _ bool) error {
	return nil
}
func (m *mockPipelineStore) PublishPipeline(_ context.Context, _, _, _ string, _ map[string]string) error {
	return nil
}
func (m *mockPipelineStore) UpdatePipelineRetention(_ context.Context, id uuid.UUID, cfg json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retentionCalls[id] = cfg
	return nil
}
func (m *mockPipelineStore) ListSoftDeletedPipelines(_ context.Context, _ time.Time) ([]domain.Pipeline, error) {
	return m.softDeleted, nil
}
func (m *mockPipelineStore) HardDeletePipeline(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hardDeleted = append(m.hardDeleted, id)
	return nil
}

type mockLandingZoneStore struct {
	zones []domain.LandingZone
}

func (m *mockLandingZoneStore) ListZones(_ context.Context, _ api.LandingZoneFilter) ([]api.LandingZoneListItem, error) {
	return nil, nil
}
func (m *mockLandingZoneStore) GetZone(_ context.Context, _, _ string) (*api.LandingZoneDetail, error) {
	return nil, nil
}
func (m *mockLandingZoneStore) CreateZone(_ context.Context, _ *domain.LandingZone) error {
	return nil
}
func (m *mockLandingZoneStore) DeleteZone(_ context.Context, _, _ string) error { return nil }
func (m *mockLandingZoneStore) UpdateZone(_ context.Context, _, _ string, _, _, _ *string) (*domain.LandingZone, error) {
	return nil, nil
}
func (m *mockLandingZoneStore) ListFiles(_ context.Context, _ uuid.UUID) ([]domain.LandingFile, error) {
	return nil, nil
}
func (m *mockLandingZoneStore) CreateFile(_ context.Context, _ *domain.LandingFile) error {
	return nil
}
func (m *mockLandingZoneStore) GetFile(_ context.Context, _ uuid.UUID) (*domain.LandingFile, error) {
	return nil, nil
}
func (m *mockLandingZoneStore) DeleteFile(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockLandingZoneStore) GetZoneByID(_ context.Context, _ uuid.UUID) (*domain.LandingZone, error) {
	return nil, nil
}
func (m *mockLandingZoneStore) UpdateZoneLifecycle(_ context.Context, _ uuid.UUID, _ *int, _ *bool) error {
	return nil
}
func (m *mockLandingZoneStore) ListZonesWithAutoPurge(_ context.Context) ([]domain.LandingZone, error) {
	return m.zones, nil
}

type mockStorageStore struct {
	mu      sync.Mutex
	files   map[string][]api.FileInfo // prefix → files
	deleted []string
}

func newMockStorageStore() *mockStorageStore {
	return &mockStorageStore{files: make(map[string][]api.FileInfo)}
}

func (m *mockStorageStore) ListFiles(_ context.Context, prefix string) ([]api.FileInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.files[prefix], nil
}
func (m *mockStorageStore) ReadFile(_ context.Context, _ string) (*api.FileContent, error) {
	return nil, nil
}
func (m *mockStorageStore) WriteFile(_ context.Context, _ string, _ []byte) (string, error) {
	return "", nil
}
func (m *mockStorageStore) DeleteFile(_ context.Context, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleted = append(m.deleted, path)
	return nil
}
func (m *mockStorageStore) StatFile(_ context.Context, _ string) (*api.FileInfo, error) {
	return nil, nil
}
func (m *mockStorageStore) ReadFileVersion(_ context.Context, _, _ string) (*api.FileContent, error) {
	return nil, nil
}

type mockAuditStore struct {
	deleted int
}

func (m *mockAuditStore) Log(_ context.Context, _, _, _, _, _ string) error { return nil }
func (m *mockAuditStore) List(_ context.Context, _, _ int) ([]domain.AuditEntry, error) {
	return nil, nil
}
func (m *mockAuditStore) DeleteOlderThan(_ context.Context, _ time.Time) (int, error) {
	m.deleted = 42
	return 42, nil
}

type mockNessieClient struct {
	branches []NessieBranch
	deleted  []string
}

func (m *mockNessieClient) ListBranches(_ context.Context) ([]NessieBranch, error) {
	return m.branches, nil
}
func (m *mockNessieClient) DeleteBranch(_ context.Context, name, _ string) error {
	m.deleted = append(m.deleted, name)
	return nil
}

// ── Tests ─────────────────────────────────────────────────────

func TestPruneRuns_DeletesExcess(t *testing.T) {
	cfg := domain.DefaultRetentionConfig()
	settings := newMockSettingsStore(cfg)
	runs := newMockRunStore()
	pipelines := newMockPipelineStore()

	p1 := domain.Pipeline{ID: uuid.New(), Namespace: "default", Layer: "bronze", Name: "test"}
	pipelines.pipelines = []domain.Pipeline{p1}

	r := New(settings, runs, pipelines, nil, nil, nil, nil)
	status := r.tick(context.Background())

	assert.Equal(t, 8, status.RunsPruned) // 5 from limit + 3 from age
	assert.Equal(t, cfg.RunsMaxPerPipeline, runs.deletedBeyondLimit[p1.ID])
}

func TestPruneRuns_PreservesActive(t *testing.T) {
	cfg := domain.DefaultRetentionConfig()
	cfg.RunsMaxPerPipeline = 50
	settings := newMockSettingsStore(cfg)
	runs := newMockRunStore()
	pipelines := newMockPipelineStore()

	p1 := domain.Pipeline{ID: uuid.New()}
	pipelines.pipelines = []domain.Pipeline{p1}

	r := New(settings, runs, pipelines, nil, nil, nil, nil)
	r.tick(context.Background())

	assert.Equal(t, 50, runs.deletedBeyondLimit[p1.ID])
}

func TestFailStuckRuns(t *testing.T) {
	cfg := domain.DefaultRetentionConfig()
	cfg.StuckRunTimeoutMinutes = 60

	settings := newMockSettingsStore(cfg)
	runs := newMockRunStore()
	stuckRun := domain.Run{
		ID:        uuid.New(),
		Status:    domain.RunStatusRunning,
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	runs.runs = []domain.Run{stuckRun}

	r := New(settings, runs, nil, nil, nil, nil, nil)
	status := r.tick(context.Background())

	assert.Equal(t, 1, status.RunsFailed)
	assert.Equal(t, domain.RunStatusFailed, runs.runs[0].Status)
}

func TestPurgeSoftDeletedPipelines(t *testing.T) {
	cfg := domain.DefaultRetentionConfig()
	cfg.SoftDeletePurgeDays = 7

	settings := newMockSettingsStore(cfg)
	pipelines := newMockPipelineStore()
	deleted := time.Now().Add(-10 * 24 * time.Hour)
	p := domain.Pipeline{ID: uuid.New(), S3Path: "test/path", DeletedAt: &deleted}
	pipelines.softDeleted = []domain.Pipeline{p}

	storage := newMockStorageStore()

	r := New(settings, nil, pipelines, nil, storage, nil, nil)
	status := r.tick(context.Background())

	assert.Equal(t, 1, status.PipelinesPurged)
	assert.Contains(t, pipelines.hardDeleted, p.ID)
}

func TestCleanOrphanBranches(t *testing.T) {
	cfg := domain.DefaultRetentionConfig()
	settings := newMockSettingsStore(cfg)

	runID := uuid.New()
	orphanRunID := uuid.New()

	runs := newMockRunStore()
	// Active run exists (created recently — not stuck)
	runs.runs = []domain.Run{
		{ID: runID, Status: domain.RunStatusRunning, CreatedAt: time.Now()},
	}

	nessie := &mockNessieClient{
		branches: []NessieBranch{
			{Name: "main", Hash: "abc"},
			{Name: "run-" + runID.String(), Hash: "def"},        // active — should NOT be deleted
			{Name: "run-" + orphanRunID.String(), Hash: "ghi"},   // orphan — should be deleted
		},
	}

	r := New(settings, runs, nil, nil, nil, nil, nessie)
	status := r.tick(context.Background())

	assert.Equal(t, 1, status.BranchesCleaned)
	assert.Contains(t, nessie.deleted, "run-"+orphanRunID.String())
	assert.NotContains(t, nessie.deleted, "run-"+runID.String())
}

func TestPurgeProcessedLZFiles(t *testing.T) {
	cfg := domain.DefaultRetentionConfig()
	settings := newMockSettingsStore(cfg)

	maxAge := 7
	zones := &mockLandingZoneStore{
		zones: []domain.LandingZone{
			{ID: uuid.New(), Namespace: "default", Name: "uploads", AutoPurge: true, ProcessedMaxAgeDays: &maxAge},
		},
	}

	oldTime := time.Now().Add(-10 * 24 * time.Hour)
	storage := newMockStorageStore()
	storage.files["default/landing/uploads/_processed/"] = []api.FileInfo{
		{Path: "default/landing/uploads/_processed/old-run/file.csv", Modified: oldTime},
		{Path: "default/landing/uploads/_processed/recent/file.csv", Modified: time.Now()},
	}

	r := New(settings, nil, nil, zones, storage, nil, nil)
	status := r.tick(context.Background())

	assert.Equal(t, 1, status.LZFilesCleaned)
	assert.Contains(t, storage.deleted, "default/landing/uploads/_processed/old-run/file.csv")
}

func TestPruneAuditLog(t *testing.T) {
	cfg := domain.DefaultRetentionConfig()
	cfg.AuditLogMaxAgeDays = 30

	settings := newMockSettingsStore(cfg)
	audit := &mockAuditStore{}

	r := New(settings, nil, nil, nil, nil, audit, nil)
	status := r.tick(context.Background())

	assert.Equal(t, 42, status.AuditPruned)
}

func TestRunNow_ReturnsStatus(t *testing.T) {
	cfg := domain.DefaultRetentionConfig()
	settings := newMockSettingsStore(cfg)
	audit := &mockAuditStore{}

	r := New(settings, nil, nil, nil, nil, audit, nil)
	status, err := r.RunNow(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, status)
	assert.Equal(t, 42, status.AuditPruned)
}

func TestStartStop(t *testing.T) {
	cfg := domain.DefaultRetentionConfig()
	cfg.ReaperIntervalMinutes = 1

	settings := newMockSettingsStore(cfg)
	r := New(settings, nil, nil, nil, nil, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)

	// Give it a moment, then stop
	time.Sleep(50 * time.Millisecond)
	cancel()
	r.Stop()
	// If we get here without hanging, the test passes
}

func TestTaskIsolation_PanicDoesNotCrash(t *testing.T) {
	cfg := domain.DefaultRetentionConfig()
	settings := newMockSettingsStore(cfg)

	// Create a reaper with nil stores — some tasks will panic
	r := New(settings, nil, nil, nil, nil, nil, nil)

	// Should not panic
	status := r.tick(context.Background())
	assert.NotNil(t, status)
}
