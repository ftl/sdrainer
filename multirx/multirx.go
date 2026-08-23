// Package multirx holds what a source needs when it runs more than one receiver at the same time,
// thus one pipeline for each receiver.
//
// Each source of SDRainer gives one IQ stream to one pipeline. A device with more than one receiver
// gives one stream for each of them, and each stream needs a pipeline of its own: the pipeline holds
// the state of one band, and two bands in one pipeline hold no meaning.
//
// **Four rules hold for each such source**, and a measurement or a defect gave each of them:
//
//   - The id of a channel carries the number of its receiver. Each pipeline counts its channels
//     from 1, so two receivers give the same id to two different stations, and the id is the value
//     with which a consumer holds one channel apart from each other one. WithReceiverIndex does
//     that work.
//   - Only the **first** receiver writes to the scope. The scope shows one spectrum and one
//     waterfall, and the frames of two receivers in one stream give a picture that no consumer can
//     take apart. Each other receiver gets a core.NullScopeService.
//   - Only the **first** receiver goes into a recording. Two IQ streams in one file are not two
//     streams any more: the samples stand one after the other, and no consumer can separate them.
//   - The channel service and the spotter take each receiver. That is the purpose of more than one
//     receiver.
//
// doc/architecture.md, section 8.1, holds the same rules with the reason of each one.
package multirx

import (
	"strconv"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/dsp"
)

// WithReceiverIndex gives a channel service that puts the number of the receiver in front of the id
// of each channel, and that then gives the event to the next service.
//
// A source with one receiver needs it not: there is nothing to hold apart, and the id then stays
// the id that each other source of SDRainer gives.
func WithReceiverIndex[F dsp.Number](index int, next core.ChannelService[F]) core.ChannelService[F] {
	return &indexedChannelService[F]{index: index, next: next}
}

type indexedChannelService[F dsp.Number] struct {
	index int
	next  core.ChannelService[F]
}

// withIndex gives the channel the id that holds the number of its receiver, as "0-1" and "1-1".
func (s *indexedChannelService[F]) withIndex(channel core.Channel[F]) core.Channel[F] {
	channel.ID = core.ChannelID(strconv.Itoa(s.index) + "-" + string(channel.ID))
	return channel
}

func (s *indexedChannelService[F]) Active() bool {
	return s.next.Active()
}

func (s *indexedChannelService[F]) ChannelCreated(channel core.Channel[F]) {
	s.next.ChannelCreated(s.withIndex(channel))
}

func (s *indexedChannelService[F]) ChannelDestroyed(channel core.Channel[F]) {
	s.next.ChannelDestroyed(s.withIndex(channel))
}

func (s *indexedChannelService[F]) ChannelStateChanged(channel core.Channel[F]) {
	s.next.ChannelStateChanged(s.withIndex(channel))
}

func (s *indexedChannelService[F]) ChannelCharacterReceived(channel core.Channel[F], character rune, offset int64) {
	s.next.ChannelCharacterReceived(s.withIndex(channel), character, offset)
}

func (s *indexedChannelService[F]) ChannelRunningCallsignDetected(channel core.Channel[F]) {
	s.next.ChannelRunningCallsignDetected(s.withIndex(channel))
}

func (s *indexedChannelService[F]) ChannelQualityChanged(channel core.Channel[F]) {
	s.next.ChannelQualityChanged(s.withIndex(channel))
}

// ScopeOf gives the scope service of the receiver with the given index. Only the first receiver
// writes to the scope, see the rules of this package.
func ScopeOf(index int, scope core.ScopeService) core.ScopeService {
	if index == 0 || scope == nil {
		return scope
	}
	return &core.NullScopeService{}
}

// FanOut gives a channel service that gives each event to each of the given services. A source uses
// it when it needs the events itself, beside the service of the consumer.
func FanOut[F dsp.Number](services ...core.ChannelService[F]) core.ChannelService[F] {
	return fanOut[F](services)
}

type fanOut[F dsp.Number] []core.ChannelService[F]

func (f fanOut[F]) Active() bool {
	for _, service := range f {
		if service.Active() {
			return true
		}
	}
	return false
}

func (f fanOut[F]) ChannelCreated(channel core.Channel[F]) {
	for _, service := range f {
		service.ChannelCreated(channel)
	}
}

func (f fanOut[F]) ChannelDestroyed(channel core.Channel[F]) {
	for _, service := range f {
		service.ChannelDestroyed(channel)
	}
}

func (f fanOut[F]) ChannelStateChanged(channel core.Channel[F]) {
	for _, service := range f {
		service.ChannelStateChanged(channel)
	}
}

func (f fanOut[F]) ChannelCharacterReceived(channel core.Channel[F], character rune, offset int64) {
	for _, service := range f {
		service.ChannelCharacterReceived(channel, character, offset)
	}
}

func (f fanOut[F]) ChannelRunningCallsignDetected(channel core.Channel[F]) {
	for _, service := range f {
		service.ChannelRunningCallsignDetected(channel)
	}
}

func (f fanOut[F]) ChannelQualityChanged(channel core.Channel[F]) {
	for _, service := range f {
		service.ChannelQualityChanged(channel)
	}
}
