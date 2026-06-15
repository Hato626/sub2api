package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type defaultGroupRepoStub struct {
	groupRepoNoop
	groups       []Group
	err          error
	calls        int
	seenPlatform string
}

func (s *defaultGroupRepoStub) ListActiveByPlatform(_ context.Context, platform string) ([]Group, error) {
	s.calls++
	s.seenPlatform = platform
	if s.err != nil {
		return nil, s.err
	}
	return s.groups, nil
}

func TestAdminServiceDefaultGroupIDsForPlatform(t *testing.T) {
	t.Run("prefers named platform default group", func(t *testing.T) {
		repo := &defaultGroupRepoStub{groups: []Group{
			{ID: 7, Name: "custom", Platform: PlatformOpenAI},
			{ID: 9, Name: PlatformOpenAI + "-default", Platform: PlatformOpenAI},
		}}
		svc := &adminServiceImpl{groupRepo: repo}

		groupIDs := svc.defaultGroupIDsForPlatform(context.Background(), PlatformOpenAI)

		require.Equal(t, []int64{9}, groupIDs)
		require.Equal(t, 1, repo.calls)
		require.Equal(t, PlatformOpenAI, repo.seenPlatform)
	})

	t.Run("falls back to sole active group", func(t *testing.T) {
		repo := &defaultGroupRepoStub{groups: []Group{
			{ID: 2, Name: "自用", Platform: PlatformOpenAI},
		}}
		svc := &adminServiceImpl{groupRepo: repo}

		groupIDs := svc.defaultGroupIDsForPlatform(context.Background(), PlatformOpenAI)

		require.Equal(t, []int64{2}, groupIDs)
	})

	t.Run("does not guess when multiple non-default groups exist", func(t *testing.T) {
		repo := &defaultGroupRepoStub{groups: []Group{
			{ID: 2, Name: "team-a", Platform: PlatformOpenAI},
			{ID: 3, Name: "team-b", Platform: PlatformOpenAI},
		}}
		svc := &adminServiceImpl{groupRepo: repo}

		groupIDs := svc.defaultGroupIDsForPlatform(context.Background(), PlatformOpenAI)

		require.Nil(t, groupIDs)
	})

	t.Run("ignores repository errors", func(t *testing.T) {
		repo := &defaultGroupRepoStub{err: errors.New("boom")}
		svc := &adminServiceImpl{groupRepo: repo}

		groupIDs := svc.defaultGroupIDsForPlatform(context.Background(), PlatformOpenAI)

		require.Nil(t, groupIDs)
	})
}
