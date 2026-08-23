// Package hpsdr takes the IQ streams of a device that speaks the openHPSDR protocol 1, and it gives
// one stream to one pipeline. The original devices of openHPSDR and the Hermes-Lite 2 both speak
// that protocol, see doc/hpsdr_plan.md.
package hpsdr

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jancona/hpsdr"
	"github.com/jancona/hpsdr/protocol1"

	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/iq"
	"github.com/ftl/sdrainer/multirx"
	"github.com/ftl/sdrainer/pipeline"
)

const (
	// defaultSampleRate is the rate that each receiver of the device uses. The device gives 48, 96,
	// 192 and 384 kHz, and each receiver of one device uses the same one.
	defaultSampleRate = 48000

	// defaultPort is the port of the protocol.
	defaultPort = 1024

	// transmitSampleRate is the rate of the stream from the PC to the device. It is 48 kHz whatever
	// the rate of the receivers is: the microphone and the transmitter of the protocol always run
	// at that rate, see doc/hpsdr_plan.md, section 1.1.
	transmitSampleRate = 48000
)

type (
	ChannelService = core.ChannelService[int]
	Spotter        = core.Spotter[int]
)

// supportedSampleRates are the rates that the device gives and that SDRainer measured. The device
// also gives 384000, and no measurement of SDRainer covers that rate: doc/hpsdr_plan.md, section 4.
var supportedSampleRates = []int{48000, 96000, 192000}

// Options holds what the command line gives.
type Options struct {
	// Host is the address of the device, with or without a port. An empty value gives a discovery
	// on the local network, and the first device that answers.
	Host string

	// CenterFrequencies holds one frequency for each receiver, in Hz.
	CenterFrequencies []int

	SampleRate    int
	PeakThreshold float64
}

// radio is the part of the device that this package uses. protocol1.Radio implements it, and a test
// gives an implementation of its own.
type radio interface {
	SetSampleRate(speed uint) error
	AddReceiver(func([]hpsdr.ReceiveSample)) (hpsdr.Receiver, error)
	SendSamples([]hpsdr.TransmitSample) error
	TransmitSamplesPerMessage() uint
	Start() error
	Stop() error
	Close()
}

type Process struct {
	radio   radio
	options Options

	scope          core.ScopeService
	channelService ChannelService
	spotter        Spotter
	recorder       *iq.Writer

	mutex     sync.Mutex
	receivers []*receiver

	done      chan struct{}
	closeOnce sync.Once
}

// receiver holds the pipeline of one receiver of the device and the buffer of its samples.
//
// **The buffer needs no lock.** The library gives the samples of each receiver from one goroutine,
// one receiver after the other, so the callback of one receiver never runs two times at the same
// time. Pipeline.IQData asks for exactly that.
type receiver struct {
	index    int
	pipeline *pipeline.Pipeline[float32, int]
	buffer   []float32
}

// New opens the device, builds one pipeline for each frequency, and starts the streams.
func New(options Options, scope core.ScopeService, channelService ChannelService, spotter Spotter, recorder *iq.Writer) (*Process, error) {
	if err := validate(options); err != nil {
		return nil, err
	}

	device, err := discover(options.Host)
	if err != nil {
		return nil, err
	}
	log.Printf("found %s with %d receivers at %s", device.Name, device.SupportedReceivers, device.Network.Address)

	if len(options.CenterFrequencies) > device.SupportedReceivers {
		return nil, fmt.Errorf("%s holds %d receivers, and --center names %d frequencies",
			device.Name, device.SupportedReceivers, len(options.CenterFrequencies))
	}

	return newProcess(protocol1.NewRadio(device), options, scope, channelService, spotter, recorder)
}

// newProcess builds the pipelines and starts the radio. It takes the radio, so that a test needs no
// device.
func newProcess(current radio, options Options, scope core.ScopeService, channelService ChannelService, spotter Spotter, recorder *iq.Writer) (*Process, error) {
	if scope == nil {
		scope = &core.NullScopeService{}
	}
	if channelService == nil {
		channelService = &core.NullChannelService[int]{}
	}
	if spotter == nil {
		spotter = &core.NullSpotter[int]{}
	}

	result := &Process{
		radio:          current,
		options:        options,
		scope:          scope,
		channelService: channelService,
		spotter:        spotter,
		recorder:       recorder,
		done:           make(chan struct{}),
	}

	if err := current.SetSampleRate(uint(options.SampleRate)); err != nil {
		return nil, fmt.Errorf("cannot set the sample rate %d: %w", options.SampleRate, err)
	}

	for index, frequency := range options.CenterFrequencies {
		if err := result.addReceiver(index, frequency); err != nil {
			result.Close()
			return nil, err
		}
	}

	if err := current.Start(); err != nil {
		result.Close()
		return nil, fmt.Errorf("cannot start the device: %w", err)
	}

	go result.keepAlive()

	return result, nil
}

// keepAlive sends an empty stream to the device until the process stops.
//
// **The device needs it for two reasons.** Its watchdog stops the IQ stream when no packet of the
// PC arrives: a measurement with a Hermes-Lite 2 shows that without this stream the device sends
// its samples for approximately 10 seconds and then no more.
//
// The command and control bytes also ride in these packets, and the library sends the addresses one
// after the other, one address with each packet. Start sends the sample rate, the count of the
// receivers and the frequency of the **first** receiver, and each further receiver gets its
// frequency only from this stream. Without it the second receiver and each one after it stay on the
// frequency that the device had before.
//
// The samples are empty: SDRainer transmits nothing, and the packets carry only the control bytes.
func (p *Process) keepAlive() {
	perMessage := int(p.radio.TransmitSamplesPerMessage())
	if perMessage < 1 {
		log.Printf("the device takes no samples, so the stream to it stays away")
		return
	}

	// the rate of the stream to the device, thus one packet for each perMessage samples at 48 kHz
	messagesPerSecond := max(1, transmitSampleRate/perMessage)
	ticker := time.NewTicker(time.Second / time.Duration(messagesPerSecond))
	defer ticker.Stop()

	samples := make([]hpsdr.TransmitSample, perMessage)
	var failed bool
	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			err := p.radio.SendSamples(samples)
			if err != nil && !failed {
				// one message is sufficient: a device that went away gives this error for each
				// packet
				failed = true
				log.Printf("cannot send to the device: %v", err)
			}
		}
	}
}

// addReceiver builds the pipeline of one receiver and asks the device for its stream.
//
// **Only the first receiver writes to the scope and into a recording**, and each receiver gives its
// channels to the channel service and to the spotter. The package multirx holds those rules.
func (p *Process) addReceiver(index int, frequency int) error {
	config := pipeline.DefaultConfig(p.options.SampleRate, frequency)
	config.PeakThreshold = p.options.PeakThreshold

	current := &receiver{index: index}
	current.pipeline = pipeline.New[float32, int](config, multirx.ScopeOf(index, p.scope))
	if p.recorder != nil && index == 0 {
		// a nil pointer in the interface would not be nil
		current.pipeline.SetRecorder(p.recorder)
	}
	current.pipeline.SetSpotter(p.spotter)
	current.pipeline.Notify(p.channelServiceOf(index))
	current.pipeline.Start()

	p.mutex.Lock()
	p.receivers = append(p.receivers, current)
	p.mutex.Unlock()

	device, err := p.radio.AddReceiver(func(samples []hpsdr.ReceiveSample) {
		current.buffer = toIQ(current.buffer, samples)
		current.pipeline.IQData(p.options.SampleRate, current.buffer)
	})
	if err != nil {
		return fmt.Errorf("cannot add the receiver for %d Hz: %w", frequency, err)
	}
	device.SetFrequency(uint(frequency))

	return nil
}

// channelServiceOf gives the events of one receiver to the service of the consumer. With more than
// one receiver the id of a channel carries the number of that receiver, see multirx.
func (p *Process) channelServiceOf(index int) ChannelService {
	if len(p.options.CenterFrequencies) < 2 {
		return p.channelService
	}
	return multirx.WithReceiverIndex(index, p.channelService)
}

// Close stops the streams of the device and each pipeline. A second call does nothing.
func (p *Process) Close() {
	p.closeOnce.Do(func() {
		close(p.done)

		if err := p.radio.Stop(); err != nil {
			log.Printf("cannot stop the device: %v", err)
		}
		p.radio.Close()

		p.mutex.Lock()
		defer p.mutex.Unlock()
		for _, current := range p.receivers {
			current.pipeline.Stop()
		}
		p.receivers = nil
	})
}

// discover gives the device of the given address, or the first device of the local network when the
// address is empty.
func discover(host string) (*hpsdr.Device, error) {
	if host != "" {
		device, err := hpsdr.DiscoverDevice(withoutPort(host))
		if err != nil {
			return nil, fmt.Errorf("cannot reach a device at %s: %w", host, err)
		}
		if device == nil {
			return nil, fmt.Errorf("no device answered at %s", host)
		}
		return device, nil
	}

	devices, err := hpsdr.DiscoverDevices()
	if err != nil {
		return nil, fmt.Errorf("cannot look for a device: %w", err)
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("no device answered on the local network")
	}

	return devices[0], nil
}

// withoutPort gives the address without the port. The library takes the address alone and it uses
// the port of the protocol itself.
func withoutPort(host string) string {
	for i := len(host) - 1; i >= 0; i-- {
		switch host[i] {
		case ':':
			return host[:i]
		case ']':
			return host
		}
	}
	return host
}

func validate(options Options) error {
	if len(options.CenterFrequencies) == 0 {
		return fmt.Errorf("--center must name at least one frequency")
	}
	for _, frequency := range options.CenterFrequencies {
		if frequency <= 0 {
			return fmt.Errorf("the center frequency %d is not a frequency", frequency)
		}
	}

	for _, rate := range supportedSampleRates {
		if options.SampleRate == rate {
			return nil
		}
	}

	return fmt.Errorf("the sample rate %d is not one of %v", options.SampleRate, supportedSampleRates)
}
