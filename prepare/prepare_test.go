package prepare

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ftl/sdrainer/core"
)

const testSampleRate = 12000

// seconds gives the count of the samples of the given time at the sample rate of the tests.
func seconds(value float64) int64 {
	return int64(value * testSampleRate)
}

// advance moves the clock of the collector to the given time. The clock follows the position of a
// character, so a character of any channel moves it.
func advance(c *collector, channel core.Channel[float64], to float64) {
	c.ChannelCharacterReceived(channel, 'e', seconds(to))
}

func testChannel(id core.ChannelID, frequency float64, state core.ChannelState) core.Channel[float64] {
	return core.Channel[float64]{ID: id, Frequency: frequency, State: state}
}

// TestCollectorMeasuresTheActiveTime is the base case: one channel that is ACTIVE from one position
// of the clock to another one.
func TestCollectorMeasuresTheActiveTime(t *testing.T) {
	c := newCollector(testSampleRate)
	channel := testChannel("1", 700, core.ActiveChannel)

	c.ChannelCreated(testChannel("1", 700, core.ConfirmedChannel))
	c.ChannelStateChanged(channel)
	advance(c, channel, 8)
	c.ChannelStateChanged(testChannel("1", 700, core.IdleChannel))

	result := c.transcribable()

	require.Len(t, result, 1)
	assert.InDelta(t, 8.0, result[0].activeTime.Seconds(), 0.001)
}

// TestCollectorAddsTheIntervals covers a channel that goes to IDLE and becomes ACTIVE again: the
// time of the pause does not count.
func TestCollectorAddsTheIntervals(t *testing.T) {
	c := newCollector(testSampleRate)
	active := testChannel("1", 700, core.ActiveChannel)
	idle := testChannel("1", 700, core.IdleChannel)

	c.ChannelCreated(testChannel("1", 700, core.ConfirmedChannel))
	c.ChannelStateChanged(active)
	advance(c, active, 4)
	c.ChannelStateChanged(idle)
	advance(c, active, 30) // the pause, and another channel moves the clock
	c.ChannelStateChanged(active)
	advance(c, active, 33)
	c.ChannelStateChanged(idle)

	result := c.transcribable()

	require.Len(t, result, 1)
	assert.InDelta(t, 7.0, result[0].activeTime.Seconds(), 0.001, "4 s and 3 s, and not the pause")
}

// TestCollectorClosesAnOpenIntervalAtTheEnd covers the channel that is still ACTIVE when the
// recording ends.
func TestCollectorClosesAnOpenIntervalAtTheEnd(t *testing.T) {
	c := newCollector(testSampleRate)
	channel := testChannel("1", 700, core.ActiveChannel)

	c.ChannelCreated(testChannel("1", 700, core.ConfirmedChannel))
	c.ChannelStateChanged(channel)
	advance(c, channel, 12)

	result := c.transcribable()

	require.Len(t, result, 1)
	assert.InDelta(t, 12.0, result[0].activeTime.Seconds(), 0.001)
}

// TestCollectorDropsAChannelBelowTheLimit is the filter that the command needs: a channel that
// carries a signal for a short time gives no file.
func TestCollectorDropsAChannelBelowTheLimit(t *testing.T) {
	c := newCollector(testSampleRate)
	short := testChannel("1", 700, core.ActiveChannel)
	long := testChannel("2", 1400, core.ActiveChannel)

	c.ChannelCreated(testChannel("1", 700, core.ConfirmedChannel))
	c.ChannelCreated(testChannel("2", 1400, core.ConfirmedChannel))
	c.ChannelStateChanged(short)
	c.ChannelStateChanged(long)
	advance(c, long, 4)
	c.ChannelStateChanged(testChannel("1", 700, core.IdleChannel))
	advance(c, long, 20)

	result := c.transcribable()

	require.Len(t, result, 1)
	assert.Equal(t, 1400.0, result[0].frequency)
}

// TestCollectorClosesTheIntervalOfADestroyedChannel covers the channel that goes away while it is
// ACTIVE.
func TestCollectorClosesTheIntervalOfADestroyedChannel(t *testing.T) {
	c := newCollector(testSampleRate)
	channel := testChannel("1", 700, core.ActiveChannel)

	c.ChannelCreated(testChannel("1", 700, core.ConfirmedChannel))
	c.ChannelStateChanged(channel)
	advance(c, channel, 9)
	c.ChannelDestroyed(testChannel("1", 700, core.DeadChannel))
	advance(c, testChannel("2", 1400, core.ActiveChannel), 40)

	result := c.transcribable()

	require.Len(t, result, 1)
	assert.InDelta(t, 9.0, result[0].activeTime.Seconds(), 0.001, "the time after the end does not count")
}

// TestCollectorSortsByFrequency keeps the order of the output stable, the lowest frequency first.
func TestCollectorSortsByFrequency(t *testing.T) {
	c := newCollector(testSampleRate)

	for _, frequency := range []float64{700, -1400, 200} {
		id := core.ChannelID(string(rune('a' + int(frequency/100))))
		c.ChannelCreated(testChannel(id, frequency, core.ConfirmedChannel))
		c.ChannelStateChanged(testChannel(id, frequency, core.ActiveChannel))
	}
	advance(c, testChannel("a", 700, core.ActiveChannel), 20)

	result := c.transcribable()

	require.Len(t, result, 3)
	assert.Equal(t, []float64{-1400, 200, 700}, []float64{result[0].frequency, result[1].frequency, result[2].frequency})
}

// TestCreateTranscriptionKeepsAFileThatExists is the rule that protects the work of a human.
func TestCreateTranscriptionKeepsAFileThatExists(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "test.iq_700.txt")
	require.NoError(t, os.WriteFile(filename, []byte("cq de dl1abc"), 0o644))

	require.NoError(t, createTranscription(filename))

	content, err := os.ReadFile(filename)
	require.NoError(t, err)
	assert.Equal(t, "cq de dl1abc", string(content))
}

// TestCreateTranscriptionMakesAnEmptyFile covers the normal case.
func TestCreateTranscriptionMakesAnEmptyFile(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "test.iq_700.txt")

	require.NoError(t, createTranscription(filename))

	content, err := os.ReadFile(filename)
	require.NoError(t, err)
	assert.Empty(t, content)
}
