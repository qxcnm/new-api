package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelConcurrencyCompetingRequests(t *testing.T) {
	channel := &Channel{Id: 910004, ChannelInfo: ChannelInfo{MaxConcurrency: 2}}
	start := make(chan struct{})
	results := make(chan func(), 3)
	var requests sync.WaitGroup
	for range 3 {
		requests.Go(func() {
			<-start
			release, _ := TryAcquireChannelConcurrency(channel)
			results <- release
		})
	}
	close(start)
	requests.Wait()
	close(results)

	var releases []func()
	rejected := 0
	for release := range results {
		if release == nil {
			rejected++
			continue
		}
		releases = append(releases, release)
		t.Cleanup(release)
	}
	assert.Len(t, releases, 2)
	assert.Equal(t, 1, rejected)
	assert.True(t, ChannelConcurrencyAtCapacity(channel))
	for _, release := range releases {
		release()
	}
	assert.False(t, ChannelConcurrencyAtCapacity(channel))
	release, acquired := TryAcquireChannelConcurrency(channel)
	require.True(t, acquired)
	release()
}

func TestChannelConcurrencyPreservesModelDiscovery(t *testing.T) {
	db := channelModelGroupsDatabase(t, common.DatabaseTypeSQLite, "")
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))
	channel := Channel{
		Id: 910003, Type: constant.ChannelTypeOpenAI, Name: "capacity-discovery", Key: "fixture-key",
		Status: common.ChannelStatusEnabled, Group: "default", Models: "capacity-model",
		ChannelInfo: ChannelInfo{MaxConcurrency: 1},
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "capacity-model", ChannelId: channel.Id, Enabled: true}).Error)

	for _, cached := range []bool{false, true} {
		common.MemoryCacheEnabled = cached
		InitChannelCache()
		assert.Equal(t, []string{"capacity-model"}, GetGroupEnabledModels("default"))
		release, acquired := TryAcquireChannelConcurrency(&channel)
		require.True(t, acquired)
		t.Cleanup(release)
		assert.Equal(t, []string{"capacity-model"}, GetGroupEnabledModels("default"), "cache=%t", cached)
		selected, err := GetRandomSatisfiedChannel("default", "capacity-model", 0, nil)
		require.NoError(t, err)
		assert.Nil(t, selected, "a full channel must remain unavailable for new requests")
		release()
		selected, err = GetRandomSatisfiedChannel("default", "capacity-model", 0, nil)
		require.NoError(t, err)
		require.NotNil(t, selected)
		assert.Equal(t, channel.Id, selected.Id)
	}
}

func TestTryAcquireChannelConcurrencyHonorsLimitAndIdempotentRelease(t *testing.T) {
	channel := &Channel{Id: 910001}
	channel.ChannelInfo.MaxConcurrency = 1

	firstRelease, acquired := TryAcquireChannelConcurrency(channel)
	require.True(t, acquired)
	require.NotNil(t, firstRelease)
	assert.True(t, ChannelConcurrencyAtCapacity(channel))

	secondRelease, acquired := TryAcquireChannelConcurrency(channel)
	assert.False(t, acquired)
	assert.Nil(t, secondRelease)

	firstRelease()
	firstRelease()
	assert.False(t, ChannelConcurrencyAtCapacity(channel))

	thirdRelease, acquired := TryAcquireChannelConcurrency(channel)
	require.True(t, acquired)
	thirdRelease()
}

func TestTryAcquireChannelConcurrencyZeroIsUnlimited(t *testing.T) {
	channel := &Channel{Id: 910002}
	channel.ChannelInfo.MaxConcurrency = 0

	for range 3 {
		release, acquired := TryAcquireChannelConcurrency(channel)
		require.True(t, acquired)
		require.NotNil(t, release)
		release()
	}
	assert.False(t, ChannelConcurrencyAtCapacity(channel))
}
