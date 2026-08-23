package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/notify"
)

var (
	_ core.ChannelLifecycleListener[float64] = (*lifecycleListener)(nil)
	_ core.ChannelStateListener[float64]     = (*stateListener)(nil)
	_ core.ChannelReceiveListener[float64]   = (*receiveListener)(nil)

	_ core.ChannelLifecycleListener[float64] = (*allListener)(nil)
	_ core.ChannelStateListener[float64]     = (*allListener)(nil)
	_ core.ChannelReceiveListener[float64]   = (*allListener)(nil)
)

type lifecycleListener struct {
	created   int
	destroyed int
}

func (l *lifecycleListener) ChannelCreated(core.Channel[float64])   { l.created++ }
func (l *lifecycleListener) ChannelDestroyed(core.Channel[float64]) { l.destroyed++ }

type stateListener struct {
	changed int
}

func (l *stateListener) ChannelStateChanged(core.Channel[float64]) { l.changed++ }

type receivedCharacter struct {
	channel   core.Channel[float64]
	character rune
	offset    int64
}

type receiveListener struct {
	received []receivedCharacter
}

func (l *receiveListener) ChannelCharacterReceived(channel core.Channel[float64], character rune, offset int64) {
	l.received = append(l.received, receivedCharacter{channel: channel, character: character, offset: offset})
}

type allListener struct {
	lifecycleListener
	stateListener
	receiveListener
}

type noListener struct{}

func TestEmitReachesOnlyTheMatchingListeners(t *testing.T) {
	lifecycle := &lifecycleListener{}
	state := &stateListener{}
	receive := &receiveListener{}
	all := &allListener{}

	listeners := []any{lifecycle, state, receive, all, &noListener{}}
	channel := core.Channel[float64]{ID: "1", Frequency: 7020000, WPM: 25, SNR: 18, State: core.ActiveChannel}

	notify.Emit(listeners, func(l core.ChannelLifecycleListener[float64]) { l.ChannelCreated(channel) })
	notify.Emit(listeners, func(l core.ChannelStateListener[float64]) { l.ChannelStateChanged(channel) })
	notify.Emit(listeners, func(l core.ChannelReceiveListener[float64]) { l.ChannelCharacterReceived(channel, 'c', 4096) })
	notify.Emit(listeners, func(l core.ChannelReceiveListener[float64]) { l.ChannelCharacterReceived(channel, 'q', 8192) })
	notify.Emit(listeners, func(l core.ChannelLifecycleListener[float64]) { l.ChannelDestroyed(channel) })

	t.Run("one interface", func(t *testing.T) {
		assert.Equal(t, 1, lifecycle.created, "created")
		assert.Equal(t, 1, lifecycle.destroyed, "destroyed")
		assert.Equal(t, 1, state.changed, "state changed")
		assert.Equal(t, []receivedCharacter{
			{channel: channel, character: 'c', offset: 4096},
			{channel: channel, character: 'q', offset: 8192},
		}, receive.received, "characters")
	})

	t.Run("all interfaces", func(t *testing.T) {
		assert.Equal(t, 1, all.created, "created")
		assert.Equal(t, 1, all.destroyed, "destroyed")
		assert.Equal(t, 1, all.changed, "state changed")
		assert.Len(t, all.received, 2, "characters")
	})
}

func TestTheListenerGetsACopyOfTheChannel(t *testing.T) {
	receive := &receiveListener{}
	listeners := []any{receive}
	channel := core.Channel[float64]{ID: "1", Frequency: 7020000, SNR: 18}

	notify.Emit(listeners, func(l core.ChannelReceiveListener[float64]) { l.ChannelCharacterReceived(channel, 'e', 1024) })
	require.Len(t, receive.received, 1)

	// the signal drifts and it fades, so the pipeline changes its channel
	channel.Frequency = 7020100
	channel.SNR = 6

	assert.Equal(t, 7020000.0, receive.received[0].channel.Frequency, "the frequency of the event must not move")
	assert.Equal(t, 18.0, receive.received[0].channel.SNR, "the SNR of the event must not move")
}

func TestEmitIgnoresAListenerWithoutAnInterface(t *testing.T) {
	listeners := []any{&noListener{}}

	assert.NotPanics(t, func() {
		notify.Emit(listeners, func(l core.ChannelLifecycleListener[float64]) {
			t.Error("a value that implements no interface must get no event")
		})
	})
}

func TestPipelineNotifyKeepsTheListeners(t *testing.T) {
	p := New[float32, float64](testConfig(), nil)
	lifecycle := &lifecycleListener{}

	p.Notify(lifecycle)
	p.Notify(&noListener{})

	assert.Len(t, p.listeners, 2, "Notify takes any value, also one without an interface")
	notify.Emit(p.listeners, func(l core.ChannelLifecycleListener[float64]) { l.ChannelCreated(core.Channel[float64]{}) })
	assert.Equal(t, 1, lifecycle.created)
}
