package plugins

import (
	"context"
	"testing"

	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestContextWithUser_RoundTrip(t *testing.T) {
	user := &domain.UserIdentity{
		UserID:      "u-123",
		Email:       "remy@rat.dev",
		DisplayName: "Remy",
		Roles:       []string{"admin"},
	}

	ctx := ContextWithUser(context.Background(), user)
	got := UserFromContext(ctx)

	assert.Equal(t, user, got)
}

func TestUserFromContext_Empty_ReturnsNil(t *testing.T) {
	got := UserFromContext(context.Background())
	assert.Nil(t, got)
}
