package cache_test

import (
	"sync"
	"testing"
	"time"

	"github.com/rat-data/rat/platform/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Get/Set Basics ---

func TestCache_SetAndGet_ReturnsValue(t *testing.T) {
	c := cache.New[string, string](cache.Options{TTL: 5 * time.Second, MaxEntries: 100})

	c.Set("key1", "value1")
	val, ok := c.Get("key1")

	assert.True(t, ok)
	assert.Equal(t, "value1", val)
}

func TestCache_Get_MissingKey_ReturnsFalse(t *testing.T) {
	c := cache.New[string, string](cache.Options{TTL: 5 * time.Second, MaxEntries: 100})

	val, ok := c.Get("nonexistent")

	assert.False(t, ok)
	assert.Equal(t, "", val)
}

func TestCache_Set_OverwritesExistingKey(t *testing.T) {
	c := cache.New[string, int](cache.Options{TTL: 5 * time.Second, MaxEntries: 100})

	c.Set("counter", 1)
	c.Set("counter", 2)
	val, ok := c.Get("counter")

	assert.True(t, ok)
	assert.Equal(t, 2, val)
}

// --- TTL Expiration ---

func TestCache_Get_ExpiredEntry_ReturnsFalse(t *testing.T) {
	c := cache.New[string, string](cache.Options{TTL: 10 * time.Millisecond, MaxEntries: 100})

	c.Set("ephemeral", "gone-soon")
	time.Sleep(20 * time.Millisecond)

	val, ok := c.Get("ephemeral")

	assert.False(t, ok)
	assert.Equal(t, "", val)
}

func TestCache_Get_NotYetExpired_ReturnsValue(t *testing.T) {
	c := cache.New[string, string](cache.Options{TTL: 1 * time.Second, MaxEntries: 100})

	c.Set("fresh", "still-here")
	time.Sleep(10 * time.Millisecond)

	val, ok := c.Get("fresh")

	assert.True(t, ok)
	assert.Equal(t, "still-here", val)
}

// --- Max Entries Eviction ---

func TestCache_Set_ExceedsMaxEntries_EvictsOldest(t *testing.T) {
	c := cache.New[string, string](cache.Options{TTL: 5 * time.Second, MaxEntries: 3})

	c.Set("a", "1")
	c.Set("b", "2")
	c.Set("c", "3")
	// Adding a 4th should evict the oldest ("a")
	c.Set("d", "4")

	_, ok := c.Get("a")
	assert.False(t, ok, "oldest entry 'a' should have been evicted")

	val, ok := c.Get("d")
	assert.True(t, ok)
	assert.Equal(t, "4", val)

	// b, c should still be present
	_, ok = c.Get("b")
	assert.True(t, ok)
	_, ok = c.Get("c")
	assert.True(t, ok)
}

func TestCache_Set_OverwriteDoesNotCountAsNew(t *testing.T) {
	c := cache.New[string, string](cache.Options{TTL: 5 * time.Second, MaxEntries: 3})

	c.Set("a", "1")
	c.Set("b", "2")
	c.Set("c", "3")
	// Overwrite "a" — should NOT trigger eviction
	c.Set("a", "updated")

	assert.Equal(t, 3, c.Len())

	val, ok := c.Get("a")
	assert.True(t, ok)
	assert.Equal(t, "updated", val)
}

// --- Delete ---

func TestCache_Delete_RemovesEntry(t *testing.T) {
	c := cache.New[string, string](cache.Options{TTL: 5 * time.Second, MaxEntries: 100})

	c.Set("doomed", "bye")
	c.Delete("doomed")

	_, ok := c.Get("doomed")
	assert.False(t, ok)
}

func TestCache_Delete_NonexistentKey_NoOp(t *testing.T) {
	c := cache.New[string, string](cache.Options{TTL: 5 * time.Second, MaxEntries: 100})

	// Should not panic
	c.Delete("ghost")
	assert.Equal(t, 0, c.Len())
}

// --- Clear ---

func TestCache_Clear_RemovesAllEntries(t *testing.T) {
	c := cache.New[string, string](cache.Options{TTL: 5 * time.Second, MaxEntries: 100})

	c.Set("a", "1")
	c.Set("b", "2")
	c.Set("c", "3")
	c.Clear()

	assert.Equal(t, 0, c.Len())

	_, ok := c.Get("a")
	assert.False(t, ok)
}

// --- Len ---

func TestCache_Len_ReflectsEntryCount(t *testing.T) {
	c := cache.New[string, string](cache.Options{TTL: 5 * time.Second, MaxEntries: 100})

	assert.Equal(t, 0, c.Len())

	c.Set("a", "1")
	assert.Equal(t, 1, c.Len())

	c.Set("b", "2")
	assert.Equal(t, 2, c.Len())

	c.Delete("a")
	assert.Equal(t, 1, c.Len())
}

// --- Default Options ---

func TestCache_DefaultTTL_Is30Seconds(t *testing.T) {
	c := cache.New[string, string](cache.Options{MaxEntries: 100})

	// Verify via the Options accessor that default TTL was applied
	assert.Equal(t, 30*time.Second, c.TTL())
}

func TestCache_DefaultMaxEntries_Is1000(t *testing.T) {
	c := cache.New[string, string](cache.Options{TTL: 5 * time.Second})

	assert.Equal(t, 1000, c.MaxEntries())
}

// --- Thread Safety ---

func TestCache_ConcurrentAccess_NoRace(t *testing.T) {
	c := cache.New[int, int](cache.Options{TTL: 1 * time.Second, MaxEntries: 100})

	var wg sync.WaitGroup
	const goroutines = 50
	const opsPerGoroutine = 100

	// Mix of Set, Get, Delete, Len, Clear operations
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := id*opsPerGoroutine + i
				c.Set(key, key*2)
				c.Get(key)
				c.Len()
				if i%10 == 0 {
					c.Delete(key)
				}
			}
		}(g)
	}

	wg.Wait()
	// If we reach here without a race detector complaint, test passes.
}

func TestCache_ConcurrentSetAndClear_NoRace(t *testing.T) {
	c := cache.New[int, string](cache.Options{TTL: 1 * time.Second, MaxEntries: 50})

	var wg sync.WaitGroup

	// Writer goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Set(id*100+j, "value")
			}
		}(i)
	}

	// Clearer goroutines
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				c.Clear()
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()
}

// --- Integer Keys ---

func TestCache_IntegerKeys_Work(t *testing.T) {
	c := cache.New[int, string](cache.Options{TTL: 5 * time.Second, MaxEntries: 100})

	c.Set(42, "answer")
	val, ok := c.Get(42)

	assert.True(t, ok)
	assert.Equal(t, "answer", val)
}

// --- Struct Values ---

type testStruct struct {
	Name  string
	Count int
}

func TestCache_StructValues_Work(t *testing.T) {
	c := cache.New[string, testStruct](cache.Options{TTL: 5 * time.Second, MaxEntries: 100})

	c.Set("item", testStruct{Name: "test", Count: 42})
	val, ok := c.Get("item")

	require.True(t, ok)
	assert.Equal(t, "test", val.Name)
	assert.Equal(t, 42, val.Count)
}

// --- Pointer Values ---

func TestCache_PointerValues_Work(t *testing.T) {
	c := cache.New[string, *testStruct](cache.Options{TTL: 5 * time.Second, MaxEntries: 100})

	item := &testStruct{Name: "ptr", Count: 7}
	c.Set("ptr-item", item)
	val, ok := c.Get("ptr-item")

	require.True(t, ok)
	assert.Equal(t, "ptr", val.Name)
	assert.Same(t, item, val) // same pointer
}

// --- Slice Values ---

func TestCache_SliceValues_Work(t *testing.T) {
	c := cache.New[string, []string](cache.Options{TTL: 5 * time.Second, MaxEntries: 100})

	c.Set("list", []string{"a", "b", "c"})
	val, ok := c.Get("list")

	require.True(t, ok)
	assert.Equal(t, []string{"a", "b", "c"}, val)
}

// --- Eviction Order ---

func TestCache_Eviction_RemovesOldestByInsertionOrder(t *testing.T) {
	c := cache.New[string, int](cache.Options{TTL: 5 * time.Second, MaxEntries: 3})

	c.Set("first", 1)
	time.Sleep(time.Millisecond)
	c.Set("second", 2)
	time.Sleep(time.Millisecond)
	c.Set("third", 3)

	// Trigger eviction
	c.Set("fourth", 4)

	// "first" should be evicted (oldest by insertion)
	_, ok := c.Get("first")
	assert.False(t, ok, "first should be evicted")

	// Rest should remain
	_, ok = c.Get("second")
	assert.True(t, ok, "second should remain")
	_, ok = c.Get("third")
	assert.True(t, ok, "third should remain")
	_, ok = c.Get("fourth")
	assert.True(t, ok, "fourth should remain")
}

// --- Expired Entries Cleaned On Set ---

func TestCache_ExpiredEntries_CleanedOnEviction(t *testing.T) {
	c := cache.New[string, string](cache.Options{TTL: 10 * time.Millisecond, MaxEntries: 3})

	c.Set("a", "1")
	c.Set("b", "2")
	c.Set("c", "3")

	// Wait for entries to expire
	time.Sleep(20 * time.Millisecond)

	// Adding a new entry should clean expired ones rather than evicting live ones
	c.Set("d", "4")

	val, ok := c.Get("d")
	assert.True(t, ok)
	assert.Equal(t, "4", val)
	// Expired entries should be gone
	assert.LessOrEqual(t, c.Len(), 1)
}
