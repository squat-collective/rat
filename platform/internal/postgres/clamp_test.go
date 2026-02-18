package postgres

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClampInt64ToInt32_WithinRange(t *testing.T) {
	assert.Equal(t, int32(1000), clampInt64ToInt32(1000))
	assert.Equal(t, int32(0), clampInt64ToInt32(0))
	assert.Equal(t, int32(-500), clampInt64ToInt32(-500))
	assert.Equal(t, int32(math.MaxInt32), clampInt64ToInt32(math.MaxInt32))
	assert.Equal(t, int32(math.MinInt32), clampInt64ToInt32(math.MinInt32))
}

func TestClampInt64ToInt32_Overflow(t *testing.T) {
	// Values exceeding int32 max should be clamped to MaxInt32
	assert.Equal(t, int32(math.MaxInt32), clampInt64ToInt32(math.MaxInt32+1))
	assert.Equal(t, int32(math.MaxInt32), clampInt64ToInt32(math.MaxInt64))

	// Very large duration (e.g., 3 billion ms ≈ 34 days)
	assert.Equal(t, int32(math.MaxInt32), clampInt64ToInt32(3_000_000_000))
}

func TestClampInt64ToInt32_Underflow(t *testing.T) {
	// Values below int32 min should be clamped to MinInt32
	assert.Equal(t, int32(math.MinInt32), clampInt64ToInt32(math.MinInt32-1))
	assert.Equal(t, int32(math.MinInt32), clampInt64ToInt32(math.MinInt64))
}
