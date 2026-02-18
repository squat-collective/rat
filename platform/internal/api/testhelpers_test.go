package api_test

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
)

// memoryQualityStore is an in-memory QualityStore for tests.
type memoryQualityStore struct {
	mu    sync.Mutex
	tests map[string][]api.QualityTest // key: "ns/layer/pipeline"
}

func newMemoryQualityStore() *memoryQualityStore {
	return &memoryQualityStore{tests: make(map[string][]api.QualityTest)}
}

func qualityKey(ns, layer, pipeline string) string {
	return ns + "/" + layer + "/" + pipeline
}

func (m *memoryQualityStore) ListTests(_ context.Context, ns, layer, pipeline string) ([]api.QualityTest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tests[qualityKey(ns, layer, pipeline)], nil
}

func (m *memoryQualityStore) CreateTest(_ context.Context, ns, layer, pipeline string, test api.QualityTest) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := qualityKey(ns, layer, pipeline)
	for _, t := range m.tests[key] {
		if t.Name == test.Name {
			return fmt.Errorf("test %q already exists", test.Name)
		}
	}
	m.tests[key] = append(m.tests[key], test)
	return nil
}

func (m *memoryQualityStore) DeleteTest(_ context.Context, ns, layer, pipeline, testName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := qualityKey(ns, layer, pipeline)
	tests := m.tests[key]
	for i, t := range tests {
		if t.Name == testName {
			m.tests[key] = append(tests[:i], tests[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("test %q not found", testName)
}

func (m *memoryQualityStore) RunTests(_ context.Context, ns, layer, pipeline string) ([]api.QualityTestResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := qualityKey(ns, layer, pipeline)
	tests := m.tests[key]
	var results []api.QualityTestResult
	for _, t := range tests {
		results = append(results, api.QualityTestResult{
			Name:       t.Name,
			Status:     "passed",
			Severity:   t.Severity,
			Value:      0,
			Expected:   0,
			DurationMs: 50,
		})
	}
	return results, nil
}

func (m *memoryQualityStore) ListTestCounts(_ context.Context, namespace string) (map[string]int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make(map[string]int)
	for key, tests := range m.tests {
		// key format: "ns/layer/pipeline" — convert to "ns.layer.pipeline" for lineage map
		parts := strings.SplitN(key, "/", 3)
		if len(parts) != 3 {
			continue
		}
		if namespace != "" && parts[0] != namespace {
			continue
		}
		dotKey := parts[0] + "." + parts[1] + "." + parts[2]
		result[dotKey] = len(tests)
	}
	return result, nil
}

// memoryQueryStore is an in-memory QueryStore for tests.
type memoryQueryStore struct {
	mu     sync.Mutex
	tables []api.TableInfo
}

func newMemoryQueryStore() *memoryQueryStore {
	return &memoryQueryStore{}
}

func (m *memoryQueryStore) ExecuteQuery(_ context.Context, sql, namespace string, limit int) (*api.QueryResult, error) {
	return &api.QueryResult{
		Columns:    []api.QueryColumn{{Name: "result", Type: "INTEGER"}},
		Rows:       []map[string]interface{}{{"result": 1}},
		TotalRows:  1,
		DurationMs: 10,
	}, nil
}

func (m *memoryQueryStore) ListTables(_ context.Context, namespace, layer string) ([]api.TableInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []api.TableInfo
	for _, t := range m.tables {
		if namespace != "" && t.Namespace != namespace {
			continue
		}
		if layer != "" && t.Layer != layer {
			continue
		}
		result = append(result, t)
	}
	return result, nil
}

func (m *memoryQueryStore) GetTable(_ context.Context, namespace, layer, name string) (*api.TableDetail, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, t := range m.tables {
		if t.Namespace == namespace && t.Layer == layer && t.Name == name {
			return &api.TableDetail{
				TableInfo: t,
				Columns:   []api.QueryColumn{{Name: "id", Type: "VARCHAR"}},
			}, nil
		}
	}
	return nil, nil
}

func (m *memoryQueryStore) GetBulkTableSchemas(_ context.Context) ([]api.SchemaEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var entries []api.SchemaEntry
	for _, t := range m.tables {
		entries = append(entries, api.SchemaEntry{
			Namespace: t.Namespace,
			Layer:     t.Layer,
			Name:      t.Name,
			Columns:   []api.QueryColumn{{Name: "id", Type: "VARCHAR"}},
		})
	}
	return entries, nil
}

func (m *memoryQueryStore) PreviewTable(_ context.Context, namespace, layer, name string, limit int) (*api.QueryResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, t := range m.tables {
		if t.Namespace == namespace && t.Layer == layer && t.Name == name {
			return &api.QueryResult{
				Columns:    []api.QueryColumn{{Name: "id", Type: "VARCHAR"}},
				Rows:       []map[string]interface{}{{"id": "abc"}},
				TotalRows:  1,
				DurationMs: 5,
			}, nil
		}
	}
	return &api.QueryResult{
		Columns:   []api.QueryColumn{},
		Rows:      []map[string]interface{}{},
		TotalRows: 0,
	}, nil
}

// memoryLandingZoneStore is an in-memory LandingZoneStore for tests.
type memoryLandingZoneStore struct {
	mu    sync.Mutex
	zones []api.LandingZoneListItem
	files []domain.LandingFile
}

func newMemoryLandingZoneStore() *memoryLandingZoneStore {
	return &memoryLandingZoneStore{}
}

func (m *memoryLandingZoneStore) ListZones(_ context.Context, filter api.LandingZoneFilter) ([]api.LandingZoneListItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []api.LandingZoneListItem
	for _, z := range m.zones {
		if filter.Namespace != "" && z.Namespace != filter.Namespace {
			continue
		}
		// Compute file stats
		item := z
		item.FileCount = 0
		item.TotalBytes = 0
		for _, f := range m.files {
			if f.ZoneID == z.ID {
				item.FileCount++
				item.TotalBytes += f.SizeBytes
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func (m *memoryLandingZoneStore) GetZone(_ context.Context, namespace, name string) (*api.LandingZoneDetail, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, z := range m.zones {
		if z.Namespace == namespace && z.Name == name {
			detail := &api.LandingZoneDetail{LandingZone: z.LandingZone}
			for _, f := range m.files {
				if f.ZoneID == z.ID {
					detail.FileCount++
					detail.TotalBytes += f.SizeBytes
				}
			}
			return detail, nil
		}
	}
	return nil, nil
}

func (m *memoryLandingZoneStore) CreateZone(_ context.Context, z *domain.LandingZone) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, existing := range m.zones {
		if existing.Namespace == z.Namespace && existing.Name == z.Name {
			return fmt.Errorf("landing zone %s/%s already exists", z.Namespace, z.Name)
		}
	}
	z.ID = uuid.New()
	m.zones = append(m.zones, api.LandingZoneListItem{LandingZone: *z})
	return nil
}

func (m *memoryLandingZoneStore) UpdateZone(_ context.Context, namespace, name string, description, owner, expectedSchema *string) (*domain.LandingZone, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, z := range m.zones {
		if z.Namespace != namespace || z.Name != name {
			continue
		}
		if description != nil {
			m.zones[i].LandingZone.Description = *description
		}
		if owner != nil {
			m.zones[i].LandingZone.Owner = owner
		}
		if expectedSchema != nil {
			m.zones[i].LandingZone.ExpectedSchema = *expectedSchema
		}
		lz := m.zones[i].LandingZone
		return &lz, nil
	}
	return nil, nil
}

func (m *memoryLandingZoneStore) DeleteZone(_ context.Context, namespace, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, z := range m.zones {
		if z.Namespace != namespace || z.Name != name {
			continue
		}
		// Remove associated files
		var remaining []domain.LandingFile
		for _, f := range m.files {
			if f.ZoneID != z.ID {
				remaining = append(remaining, f)
			}
		}
		m.files = remaining
		m.zones = append(m.zones[:i], m.zones[i+1:]...)
		return nil
	}
	return nil
}

func (m *memoryLandingZoneStore) ListFiles(_ context.Context, zoneID uuid.UUID) ([]domain.LandingFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []domain.LandingFile
	for _, f := range m.files {
		if f.ZoneID == zoneID {
			result = append(result, f)
		}
	}
	return result, nil
}

func (m *memoryLandingZoneStore) CreateFile(_ context.Context, f *domain.LandingFile) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	f.ID = uuid.New()
	m.files = append(m.files, *f)
	return nil
}

func (m *memoryLandingZoneStore) GetFile(_ context.Context, fileID uuid.UUID) (*domain.LandingFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, f := range m.files {
		if f.ID == fileID {
			return &f, nil
		}
	}
	return nil, nil
}

func (m *memoryLandingZoneStore) DeleteFile(_ context.Context, fileID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, f := range m.files {
		if f.ID == fileID {
			m.files = append(m.files[:i], m.files[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("file not found")
}

func (m *memoryLandingZoneStore) UpdateZoneLifecycle(_ context.Context, zoneID uuid.UUID, processedMaxAgeDays *int, autoPurge *bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, z := range m.zones {
		if z.ID == zoneID {
			if processedMaxAgeDays != nil {
				m.zones[i].LandingZone.ProcessedMaxAgeDays = processedMaxAgeDays
			}
			if autoPurge != nil {
				m.zones[i].LandingZone.AutoPurge = *autoPurge
			}
			return nil
		}
	}
	// P10-27: Return error on missing zone to match production behavior.
	return fmt.Errorf("landing zone %s not found", zoneID)
}

func (m *memoryLandingZoneStore) ListZonesWithAutoPurge(_ context.Context) ([]domain.LandingZone, error) {
	return nil, nil
}

func (m *memoryLandingZoneStore) GetZoneByID(_ context.Context, zoneID uuid.UUID) (*domain.LandingZone, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, z := range m.zones {
		if z.ID == zoneID {
			lz := z.LandingZone
			return &lz, nil
		}
	}
	return nil, nil
}

// fullTestServer creates a Server with ALL stores populated.
func fullTestServer() *api.Server {
	return &api.Server{
		Pipelines:    newMemoryPipelineStore(),
		Versions:     newMemoryVersionStore(),
		Runs:         newMemoryRunStore(),
		Namespaces:   newMemoryNamespaceStore(),
		Schedules:    newMemoryScheduleStore(),
		Storage:      newMemoryStorageStore(),
		Quality:      newMemoryQualityStore(),
		Query:        newMemoryQueryStore(),
		LandingZones: newMemoryLandingZoneStore(),
		Triggers:     newMemoryTriggerStore(),
	}
}
