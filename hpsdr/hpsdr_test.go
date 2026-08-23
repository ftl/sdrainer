package hpsdr

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jancona/hpsdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/pipeline/generator"
)

const (
	testSampleRate = 48000
	testChunk      = 63 // the count of the samples of one frame with one receiver
)

/* the device */

// fakeRadio is a device that gives no samples by itself. A test gives the samples of one receiver
// with sendTo, so it needs no hardware and no timing.
type fakeRadio struct {
	mutex     sync.Mutex
	sampleAt  []func([]hpsdr.ReceiveSample)
	frequency []uint

	sampleRate uint
	started    bool
	stopped    bool
	closed     bool

	maxReceivers int
	startErr     error

	sent    int
	sendErr error
}

func newFakeRadio() *fakeRadio {
	return &fakeRadio{maxReceivers: 8}
}

func (r *fakeRadio) SetSampleRate(speed uint) error {
	r.sampleRate = speed
	return nil
}

func (r *fakeRadio) AddReceiver(sampleFunc func([]hpsdr.ReceiveSample)) (hpsdr.Receiver, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if len(r.sampleAt) >= r.maxReceivers {
		return nil, errors.New("no receiver left")
	}
	r.sampleAt = append(r.sampleAt, sampleFunc)
	r.frequency = append(r.frequency, 0)

	return &fakeReceiver{radio: r, index: len(r.sampleAt) - 1}, nil
}

func (r *fakeRadio) SendSamples(samples []hpsdr.TransmitSample) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.sent++
	return r.sendErr
}

func (r *fakeRadio) TransmitSamplesPerMessage() uint { return 126 }

func (r *fakeRadio) sentPackets() int {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.sent
}

func (r *fakeRadio) Start() error {
	r.started = true
	return r.startErr
}

func (r *fakeRadio) Stop() error {
	r.stopped = true
	return nil
}

func (r *fakeRadio) Close() { r.closed = true }

// sendTo gives the samples to one receiver, as the reader of the library does.
func (r *fakeRadio) sendTo(index int, samples []hpsdr.ReceiveSample) {
	r.mutex.Lock()
	sampleFunc := r.sampleAt[index]
	r.mutex.Unlock()

	sampleFunc(samples)
}

func (r *fakeRadio) receivers() int {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return len(r.sampleAt)
}

type fakeReceiver struct {
	radio *fakeRadio
	index int
}

func (r *fakeReceiver) SetFrequency(frequency uint) {
	r.radio.mutex.Lock()
	defer r.radio.mutex.Unlock()
	r.radio.frequency[r.index] = frequency
}

func (r *fakeReceiver) GetFrequency() uint {
	r.radio.mutex.Lock()
	defer r.radio.mutex.Unlock()
	return r.radio.frequency[r.index]
}

func (r *fakeReceiver) Close() error   { return nil }
func (r *fakeReceiver) IsClosed() bool { return false }

/* the consumer */

type testChannelService struct {
	mutex sync.Mutex
	ids   []core.ChannelID
	text  map[core.ChannelID][]rune
	spots []string
}

func newTestChannelService() *testChannelService {
	return &testChannelService{text: make(map[core.ChannelID][]rune)}
}

func (s *testChannelService) Active() bool { return true }

func (s *testChannelService) ChannelCreated(channel core.Channel[int]) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.ids = append(s.ids, channel.ID)
}

func (s *testChannelService) ChannelDestroyed(core.Channel[int])    {}
func (s *testChannelService) ChannelStateChanged(core.Channel[int]) {}
func (s *testChannelService) ChannelQualityChanged(core.Channel[int]) {
}

func (s *testChannelService) ChannelCharacterReceived(channel core.Channel[int], character rune, _ int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.text[channel.ID] = append(s.text[channel.ID], character)
}

func (s *testChannelService) ChannelRunningCallsignDetected(core.Channel[int]) {}

func (s *testChannelService) Spot(callsign string, _ int, _ string, _ time.Time) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.spots = append(s.spots, callsign)
}

func (s *testChannelService) RemoveSpot(string) {}

func (s *testChannelService) channelIDs() []core.ChannelID {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return append([]core.ChannelID{}, s.ids...)
}

func (s *testChannelService) anyText() string {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	var result strings.Builder
	for _, text := range s.text {
		result.WriteString(string(text))
	}
	return result.String()
}

/* the samples */

// cwSamples makes the samples of a CW signal, as the device gives them: 24 bit, signed.
func cwSamples(t *testing.T, center int, offset int, seconds float64) []hpsdr.ReceiveSample {
	t.Helper()

	source := generator.New[float32, float64](generator.GeneratorConfig[float64]{
		SampleRate: testSampleRate, CenterFrequency: float64(center), NoiseLevel: 0.01, Seed: 1,
		Signals: []generator.CWSignal[float64]{
			{
				Frequency: float64(center + offset), Text: "cq a1bc a1bc test", WPM: 30,
				Amplitude: 1.0, RiseTime: 5 * time.Millisecond,
			},
		},
	})

	count := int(seconds * testSampleRate)
	iq := make([]float32, 2*count)
	source.Read(iq)

	result := make([]hpsdr.ReceiveSample, count)
	for i := range result {
		result[i] = toReceiveSample(iq[2*i], iq[2*i+1])
	}
	return result
}

// toReceiveSample makes one sample of the device out of two values between −1 and 1.
func toReceiveSample(i float32, q float32) hpsdr.ReceiveSample {
	return sample(toInt24(i), toInt24(q))
}

func toInt24(value float32) int32 {
	result := int32(value * 0x7FFFFF)
	if result > 0x7FFFFF {
		result = 0x7FFFFF
	}
	if result < -0x800000 {
		result = -0x800000
	}
	return result
}

/* the tests */

func newTestProcess(t *testing.T, radio *fakeRadio, service *testChannelService, frequencies ...int) *Process {
	t.Helper()

	process, err := newProcess(radio, Options{
		CenterFrequencies: frequencies,
		SampleRate:        testSampleRate,
		PeakThreshold:     12,
	}, nil, service, service, nil)
	require.NoError(t, err)

	return process
}

func TestProcessBuildsOnePipelineForEachFrequency(t *testing.T) {
	radio := newFakeRadio()

	process := newTestProcess(t, radio, newTestChannelService(), 7020000, 14020000, 21020000)
	defer process.Close()

	assert.Equal(t, 3, radio.receivers())
	assert.Equal(t, uint(testSampleRate), radio.sampleRate)
	assert.Equal(t, []uint{7020000, 14020000, 21020000}, radio.frequency)
	assert.True(t, radio.started)
}

func TestProcessDecodesTheStreamOfTheDevice(t *testing.T) {
	radio := newFakeRadio()
	service := newTestChannelService()
	process := newTestProcess(t, radio, service, 7020000)
	defer process.Close()

	samples := cwSamples(t, 7020000, 1000, 12)
	for i := 0; i+testChunk <= len(samples); i += testChunk {
		radio.sendTo(0, samples[i:i+testChunk])
	}
	process.Close()

	assert.Containsf(t, service.anyText(), "a1bc", "the text of the station, got %q", service.anyText())
}

// TestProcessHoldsTheChannelsOfTheReceiversApart is the rule of the package multirx: each pipeline
// counts its channels from 1, so two receivers give the same id to two different stations.
func TestProcessHoldsTheChannelsOfTheReceiversApart(t *testing.T) {
	radio := newFakeRadio()
	service := newTestChannelService()
	process := newTestProcess(t, radio, service, 7020000, 14020000)
	defer process.Close()

	for index, center := range []int{7020000, 14020000} {
		samples := cwSamples(t, center, 1000, 12)
		for i := 0; i+testChunk <= len(samples); i += testChunk {
			radio.sendTo(index, samples[i:i+testChunk])
		}
	}
	process.Close()

	ids := service.channelIDs()
	require.NotEmpty(t, ids, "the receivers must give channels")

	var first, second int
	for _, id := range ids {
		switch {
		case strings.HasPrefix(string(id), "0-"):
			first++
		case strings.HasPrefix(string(id), "1-"):
			second++
		default:
			t.Errorf("the id %q holds no receiver", id)
		}
	}
	assert.NotZero(t, first, "the first receiver must give a channel")
	assert.NotZero(t, second, "the second receiver must give a channel")
}

// TestProcessKeepsThePlainChannelIDWithOneReceiver is the other side: with one receiver there is
// nothing to hold apart, and the id stays the id that each other source of SDRainer gives.
func TestProcessKeepsThePlainChannelIDWithOneReceiver(t *testing.T) {
	radio := newFakeRadio()
	service := newTestChannelService()
	process := newTestProcess(t, radio, service, 7020000)
	defer process.Close()

	samples := cwSamples(t, 7020000, 1000, 12)
	for i := 0; i+testChunk <= len(samples); i += testChunk {
		radio.sendTo(0, samples[i:i+testChunk])
	}
	process.Close()

	ids := service.channelIDs()
	require.NotEmpty(t, ids)
	for _, id := range ids {
		assert.NotContainsf(t, string(id), "-", "the id %q must hold no receiver", id)
	}
}

func TestProcessStopsTheDeviceAndThePipelines(t *testing.T) {
	radio := newFakeRadio()
	process := newTestProcess(t, radio, newTestChannelService(), 7020000, 14020000)

	process.Close()

	assert.True(t, radio.stopped)
	assert.True(t, radio.closed)

	assert.NotPanics(t, func() { process.Close() }, "a second call does nothing")
}

func TestProcessStopsWhenTheDeviceDoesNotStart(t *testing.T) {
	radio := newFakeRadio()
	radio.startErr = errors.New("the device says no")

	_, err := newProcess(radio, Options{
		CenterFrequencies: []int{7020000},
		SampleRate:        testSampleRate,
	}, nil, nil, nil, nil)

	require.Error(t, err)
	assert.True(t, radio.closed, "the pipelines and the device must not stay open")
}

func TestProcessNeedsAReceiverForEachFrequency(t *testing.T) {
	radio := newFakeRadio()
	radio.maxReceivers = 1

	_, err := newProcess(radio, Options{
		CenterFrequencies: []int{7020000, 14020000},
		SampleRate:        testSampleRate,
	}, nil, nil, nil, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "14020000")
}

func TestValidate(t *testing.T) {
	tt := []struct {
		name    string
		options Options
		valid   bool
	}{
		{name: "one frequency", options: Options{CenterFrequencies: []int{7020000}, SampleRate: 48000}, valid: true},
		{name: "96 kHz", options: Options{CenterFrequencies: []int{7020000}, SampleRate: 96000}, valid: true},
		{name: "192 kHz", options: Options{CenterFrequencies: []int{7020000}, SampleRate: 192000}, valid: true},
		{name: "no frequency", options: Options{SampleRate: 48000}},
		{name: "a frequency of 0", options: Options{CenterFrequencies: []int{0}, SampleRate: 48000}},
		{name: "384 kHz, which SDRainer never measured", options: Options{CenterFrequencies: []int{7020000}, SampleRate: 384000}},
		{name: "a rate that the device does not give", options: Options{CenterFrequencies: []int{7020000}, SampleRate: 12000}},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			err := validate(tc.options)

			if tc.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestWithoutPort(t *testing.T) {
	tt := []struct {
		host     string
		expected string
	}{
		{host: "192.168.1.10", expected: "192.168.1.10"},
		{host: "192.168.1.10:1024", expected: "192.168.1.10"},
		{host: "hermes.local:1024", expected: "hermes.local"},
		{host: "[fe80::1]", expected: "[fe80::1]"},
		{host: "[fe80::1]:1024", expected: "[fe80::1]"},
	}

	for _, tc := range tt {
		t.Run(tc.host, func(t *testing.T) {
			assert.Equal(t, tc.expected, withoutPort(tc.host))
		})
	}
}

// TestProcessSendsAStreamToTheDevice covers the defect that a Hermes-Lite 2 showed: the device
// stops its IQ stream after approximately 10 seconds when no packet of the PC arrives, and the
// frequency of each receiver behind the first one rides in those packets as well.
func TestProcessSendsAStreamToTheDevice(t *testing.T) {
	radio := newFakeRadio()
	process := newTestProcess(t, radio, newTestChannelService(), 7020000, 14020000)
	defer process.Close()

	// the stream runs at 48 kHz with 126 samples in one packet, thus approximately 380 packets for
	// each second
	assert.Eventually(t, func() bool { return radio.sentPackets() > 10 }, time.Second, 5*time.Millisecond,
		"the process must send to the device without a pause")
}

// TestProcessStopsTheStreamToTheDevice keeps the goroutine away from a process that ended.
func TestProcessStopsTheStreamToTheDevice(t *testing.T) {
	radio := newFakeRadio()
	process := newTestProcess(t, radio, newTestChannelService(), 7020000)
	require.Eventually(t, func() bool { return radio.sentPackets() > 0 }, time.Second, 5*time.Millisecond)

	process.Close()
	time.Sleep(50 * time.Millisecond)
	before := radio.sentPackets()
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, before, radio.sentPackets(), "the stream stops with the process")
}

// TestProcessKeepsSendingAfterAnError covers the packet that does not go out: one message goes into
// the log and the stream continues, because the device can come back.
func TestProcessKeepsSendingAfterAnError(t *testing.T) {
	radio := newFakeRadio()
	radio.sendErr = errors.New("the network says no")
	process := newTestProcess(t, radio, newTestChannelService(), 7020000)
	defer process.Close()

	assert.Eventually(t, func() bool { return radio.sentPackets() > 10 }, time.Second, 5*time.Millisecond)
}
