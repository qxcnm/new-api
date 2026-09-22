package model

import "sync"

// MaxChannelConcurrency is a defensive upper bound for the administrator
// supplied per-channel concurrency setting. Zero means unlimited.
const MaxChannelConcurrency = 100000

type channelConcurrencyState struct {
	active int
}

var channelConcurrency = struct {
	sync.Mutex
	states map[int]*channelConcurrencyState
}{
	states: make(map[int]*channelConcurrencyState),
}

// ChannelConcurrencyAtCapacity reports whether a channel has no available
// request slot. It is intentionally a snapshot; callers must still acquire a
// lease before starting work because another request may win the race.
func ChannelConcurrencyAtCapacity(channel *Channel) bool {
	if channel == nil || channel.ChannelInfo.MaxConcurrency <= 0 {
		return false
	}

	channelConcurrency.Lock()
	defer channelConcurrency.Unlock()
	state := channelConcurrency.states[channel.Id]
	return state != nil && state.active >= channel.ChannelInfo.MaxConcurrency
}

// TryAcquireChannelConcurrency reserves one in-flight request slot. The
// returned release function is safe to call more than once. A nil release and
// false indicate that a limited channel is currently full.
func TryAcquireChannelConcurrency(channel *Channel) (release func(), acquired bool) {
	if channel == nil || channel.ChannelInfo.MaxConcurrency <= 0 {
		return func() {}, true
	}

	channelConcurrency.Lock()
	defer channelConcurrency.Unlock()

	state := channelConcurrency.states[channel.Id]
	if state == nil {
		state = &channelConcurrencyState{}
		channelConcurrency.states[channel.Id] = state
	}
	if state.active >= channel.ChannelInfo.MaxConcurrency {
		return nil, false
	}
	state.active++

	var once sync.Once
	return func() {
		once.Do(func() {
			channelConcurrency.Lock()
			defer channelConcurrency.Unlock()
			state.active--
			if state.active <= 0 {
				delete(channelConcurrency.states, channel.Id)
			}
		})
	}, true
}
