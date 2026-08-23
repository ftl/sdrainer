package multirx

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ftl/sdrainer/core"
)

// recordingService collects the events that reach it.
type recordingService struct {
	active     bool
	created    []core.ChannelID
	destroyed  []core.ChannelID
	states     []core.ChannelID
	characters []core.ChannelID
	callsigns  []core.ChannelID
	qualities  []core.ChannelID
}

func (s *recordingService) Active() bool { return s.active }

func (s *recordingService) ChannelCreated(channel core.Channel[float64]) {
	s.created = append(s.created, channel.ID)
}

func (s *recordingService) ChannelDestroyed(channel core.Channel[float64]) {
	s.destroyed = append(s.destroyed, channel.ID)
}

func (s *recordingService) ChannelStateChanged(channel core.Channel[float64]) {
	s.states = append(s.states, channel.ID)
}

func (s *recordingService) ChannelCharacterReceived(channel core.Channel[float64], _ rune, _ int64) {
	s.characters = append(s.characters, channel.ID)
}

func (s *recordingService) ChannelRunningCallsignDetected(channel core.Channel[float64]) {
	s.callsigns = append(s.callsigns, channel.ID)
}

func (s *recordingService) ChannelQualityChanged(channel core.Channel[float64]) {
	s.qualities = append(s.qualities, channel.ID)
}

func testChannel(id core.ChannelID) core.Channel[float64] {
	return core.Channel[float64]{ID: id, Frequency: 7028000}
}

// TestWithReceiverIndexHoldsTheChannelsApart is the reason for this package: each pipeline counts
// its channels from 1, so two receivers give the same id to two different stations.
func TestWithReceiverIndexHoldsTheChannelsApart(t *testing.T) {
	next := &recordingService{}
	first := WithReceiverIndex(0, core.ChannelService[float64](next))
	second := WithReceiverIndex(1, core.ChannelService[float64](next))

	first.ChannelCreated(testChannel("1"))
	second.ChannelCreated(testChannel("1"))

	assert.Equal(t, []core.ChannelID{"0-1", "1-1"}, next.created)
}

// TestWithReceiverIndexHoldsEachEvent covers each event of a channel: a consumer holds a channel by
// its id, so each event must name the same one.
func TestWithReceiverIndexHoldsEachEvent(t *testing.T) {
	next := &recordingService{}
	service := WithReceiverIndex(2, core.ChannelService[float64](next))

	channel := testChannel("7")
	service.ChannelCreated(channel)
	service.ChannelDestroyed(channel)
	service.ChannelStateChanged(channel)
	service.ChannelCharacterReceived(channel, 'a', 4711)
	service.ChannelRunningCallsignDetected(channel)
	service.ChannelQualityChanged(channel)

	expected := []core.ChannelID{"2-7"}
	assert.Equal(t, expected, next.created)
	assert.Equal(t, expected, next.destroyed)
	assert.Equal(t, expected, next.states)
	assert.Equal(t, expected, next.characters)
	assert.Equal(t, expected, next.callsigns)
	assert.Equal(t, expected, next.qualities)
}

func TestWithReceiverIndexGivesTheStateOfTheNextService(t *testing.T) {
	next := &recordingService{active: true}
	service := WithReceiverIndex(0, core.ChannelService[float64](next))

	assert.True(t, service.Active())
}

// TestScopeOfGivesTheScopeToTheFirstReceiverOnly holds the rule of the scope: it shows one spectrum,
// and the frames of two receivers in one stream give a picture that no consumer can take apart.
func TestScopeOfGivesTheScopeToTheFirstReceiverOnly(t *testing.T) {
	scope := &core.NullScopeService{}

	assert.Same(t, scope, ScopeOf(0, scope), "the first receiver keeps the scope")
	assert.NotSame(t, scope, ScopeOf(1, scope), "each other receiver writes to nothing")
	assert.NotNil(t, ScopeOf(1, scope))
}

func TestScopeOfWithoutAScope(t *testing.T) {
	assert.Nil(t, ScopeOf(0, nil))
	assert.Nil(t, ScopeOf(1, nil), "a source without a scope gives none to any receiver")
}

// TestFanOutGivesEachEventToEachService covers the source that needs the events itself, beside the
// service of the consumer.
func TestFanOutGivesEachEventToEachService(t *testing.T) {
	first := &recordingService{}
	second := &recordingService{}
	service := FanOut(core.ChannelService[float64](first), core.ChannelService[float64](second))

	channel := testChannel("1")
	service.ChannelCreated(channel)
	service.ChannelDestroyed(channel)
	service.ChannelStateChanged(channel)
	service.ChannelCharacterReceived(channel, 'a', 0)
	service.ChannelRunningCallsignDetected(channel)
	service.ChannelQualityChanged(channel)

	for _, current := range []*recordingService{first, second} {
		assert.Equal(t, []core.ChannelID{"1"}, current.created)
		assert.Equal(t, []core.ChannelID{"1"}, current.destroyed)
		assert.Equal(t, []core.ChannelID{"1"}, current.states)
		assert.Equal(t, []core.ChannelID{"1"}, current.characters)
		assert.Equal(t, []core.ChannelID{"1"}, current.callsigns)
		assert.Equal(t, []core.ChannelID{"1"}, current.qualities)
	}
}

func TestFanOutIsActiveWhenOneServiceIsActive(t *testing.T) {
	quiet := &recordingService{}
	active := &recordingService{active: true}

	assert.False(t, FanOut(core.ChannelService[float64](quiet)).Active())
	assert.True(t, FanOut(core.ChannelService[float64](quiet), core.ChannelService[float64](active)).Active())
}

// TestFanOutKeepsTheOrderOfTheServices matters for the TCI device: the service of the consumer sees
// an event before the process draws it on the panorama.
func TestFanOutKeepsTheOrderOfTheServices(t *testing.T) {
	var order []string
	first := &orderedService{name: "first", order: &order}
	second := &orderedService{name: "second", order: &order}

	FanOut(core.ChannelService[float64](first), core.ChannelService[float64](second)).ChannelCreated(testChannel("1"))

	require.Len(t, order, 2)
	assert.Equal(t, []string{"first", "second"}, order)
}

type orderedService struct {
	core.NullChannelService[float64]
	name  string
	order *[]string
}

func (s *orderedService) ChannelCreated(core.Channel[float64]) {
	*s.order = append(*s.order, s.name)
}
