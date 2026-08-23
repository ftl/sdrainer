# Glossary

This file gives the signal processing terms that the code and the documentation
use. The order of the terms is approximately the order in which they build on
each other.

## STFT — Short-Time Fourier Transform

The STFT is a time-frequency analysis. It shows how the frequency content of a
signal changes with time. It does not make one FFT of the complete signal.
Instead it cuts the signal into short frames. The frames overlap. It multiplies
each frame by a window function. The window function decreases the spectral
leakage at the edges of the frame. Then it makes one FFT for each frame. The
result is one spectrum for each frame. Put the spectra together, and you get a
**spectrogram**. A spectrogram shows the frequency on one axis and the time on
the other axis.

Three parameters set the behavior of an STFT: the frame length, the hop and the
window function. The hop is the distance between two frames.

The frame length sets the frequency resolution. One bin covers
`sample_rate / frame_length` Hz. The frame length also sets the time resolution,
because the FFT mixes together all events inside one frame.

The hop sets how many spectra you get for each second. A hop of one quarter of
the frame length gives an overlap of 4 times.

A long frame finds a weak carrier. But it also makes fast keying less clear. The
two goals are in conflict. Many systems use two different STFTs, one for each
goal.

## Hann window

A window function is a curve of weights. Multiply each frame by this curve before
the FFT.

A frame without a window has hard edges, because you cut it out of a longer
signal. The FFT interprets these steps as high-frequency energy. The energy
spreads across all bins. This effect is **spectral leakage**. A window curve
decreases to zero at the two ends of the frame. Then the edges have no step.

The Hann window is a raised cosine, `0.5 · (1 − cos(2πi/N))`. It gives a good
balance between the width of the main lobe and the level of the side lobes. The
main lobe sets the frequency resolution. The side lobes cause the leakage.

The main lobe of the Hann window is 1.5 bins wide. Thus one bin collects
approximately 1.5 times the noise of a nominal bin. The window also removes
energy at the edges of the frame. An overlap of the frames replaces this energy.
An overlap of 4 times is a usual value.

## Biquad

A biquad is a digital IIR filter of the second order. It is the basic element of
most audio filters. The name is short for biquadratic: its transfer function is
the ratio of two quadratic functions. Thus it has two feed-forward taps and two
feedback taps. The filter calculates each output sample from the two last input
samples and the two last output samples:

```
y[n] = b0·x[n] + b1·x[n−1] + b2·x[n−2] − a1·y[n−1] − a2·y[n−2]
```

Five coefficients (`b0,b1,b2,a1,a2`) set the response. Divide all coefficients by
`a0` first. The possible responses are lowpass, highpass, bandpass, notch,
peaking and shelf. The usual source for the coefficient formulas is the
[RBJ cookbook](https://www.w3.org/TR/audio-eq-cookbook/).

A cascade of several biquads makes the roll-off steeper. This is the cheap method
to make a narrow CW audio filter. Four passes of one bandpass section is a usual
arrangement.

## CIC — Cascaded Integrator-Comb

A CIC filter is a decimation filter without multipliers (Hogenauer, 1981). It is
the cheap method to change a wideband stream into a narrowband stream. It uses
only additions and subtractions. For a decimation factor `R` it has three parts:

1. **Integrator** section — `N` accumulators at the *high* sample rate. Each one
   calculates `y[n] = y[n−1] + x[n]`.
2. **Downsampler** — keep 1 sample from each `R` samples.
3. **Comb** section — `N` differentiators at the *low* sample rate. Each one
   calculates `y[n] = x[n] − x[n−M]`. The differential delay `M` is 1 or 2.

The result is the same as a moving average of the length `R·M`, applied `N`
times. But the filter makes only `N` additions for each input sample. `N` sets
the attenuation in the stopband. A usual value for `N` is 3 to 5.

Two properties are important:

- **Passband droop.** The response is similar to a sinc function, `(sin/sin)^N`.
  Thus the top of the passband falls. Put a short FIR compensation filter after
  the CIC. Or keep the wanted signal near DC, where the droop is very small.
- **Overflow by design.** The accumulators increase without a limit, and they
  must wrap around. Use fixed-point arithmetic in two's complement. Make the word
  width at least `N · log2(R · M) + input_bits`. The comb section removes the
  wrap exactly. Do not use floating-point accumulators here, because they lose
  precision.

A cascade of half-band FIR decimators does the same work. It has no droop and it
has a linear phase, but it needs multipliers for each tap. A usual solution uses
both types: the CIC makes the large rate reduction, and one short FIR gives the
final channel shape. Refer to the per-signal extraction stage in
[Research: CW Multi-Signal Decode Pipeline](./research_pipeline.md).

## NCO — Numerically Controlled Oscillator

An NCO is an oscillator in software. It gives the sine and the cosine of one
frequency, one value for each sample. It needs no table of values and no filter.

An NCO holds one value: the **phase**. Each sample adds a constant step to that
phase:

```
step  = 2π · frequency / sample_rate
phase = phase + step
```

The sine and the cosine of the phase give the two parts of the oscillator. Keep
the phase inside one turn, between 0 and 2π. A phase that increases without
a limit loses precision after some minutes of a stream.

### The NCO moves a signal

This is the usual work of an NCO. Multiply each complex sample by the value of
the oscillator. The signal then moves by the frequency of the oscillator:

```
I_out = I · cos(phase) − Q · sin(phase)
Q_out = I · sin(phase) + Q · cos(phase)
```

A **negative** frequency moves a signal down. A signal at +1000 Hz that you
multiply with an NCO at −1000 Hz lies at 0 Hz after the multiplication. A
**positive** frequency moves the signal up.

The multiplication changes no level. The value of the oscillator has the length
1, so the multiplication turns each sample and it does not make it larger or
smaller.

### Where SDRainer uses it

The `listen` command takes one signal out of a recording with two oscillators:

1. An NCO at the negative offset of the signal moves that signal to 0 Hz.
2. A lowpass filter removes each other signal.
3. An NCO at the pitch of a tone moves the signal from 0 Hz to a frequency that
   an ear hears.

The generator of the test signals uses the same phase accumulator to put each
CW signal on its frequency.

## MFCC — Mel-Frequency Cepstral Coefficients

The MFCCs are the classic compact feature set for speech recognition. These are
the steps:

1. Take the STFT magnitude spectrum.
2. Change the frequency axis to the **mel scale**. The mel scale is a perceptual
   scale. It has a finer resolution at low frequencies, where the human ear is
   more sensitive.
3. Take the logarithm of the energy in each mel band.
4. Apply a DCT. The DCT is a transform of the cepstrum type.
5. Keep approximately the first 13 coefficients.

The result is a small vector for each frame. The elements of the vector are
almost independent of each other. The vector holds the spectral envelope, but not
the pitch.

Neural CW decoders usually do not use MFCCs. A CW signal has only one tone. Thus
the STFT magnitude spectrogram is already a small and good input. This file
includes the MFCC only because the word "Cepstral" in CMVN comes from this
history. Classic speech systems normalize cepstral features. Other systems kept
the CMVN name for the equivalent step. Refer to CMVN below.

## Cepstrum

The cepstrum is the spectrum of a spectrum. The word is an anagram of *spectrum*.
Two more anagrams of the same type are *quefrency* (from *frequency*) and
*lifter* (from *filter*). This is the calculation:

```
cepstrum = IFFT( log( |FFT(signal)| ) )
```

Make an FFT of the signal. Take the magnitude. Take the logarithm. Then make an
inverse FFT. The result is on a time-like axis with the name **quefrency**.

The cepstrum is useful because many signals are a product: a fast excitation
multiplies a slow envelope. For speech, the excitation is the pitch of the vocal
cords, and the envelope is the shape of the vocal tract. The logarithm changes
the product into a sum. The inverse FFT then separates the two parts. The slow
envelope goes to a low quefrency. The periodic excitation makes a sharp peak at a
higher quefrency. Thus you can use the cepstrum for pitch detection, and to find
vocal-tract features. This is the basis of the MFCC.

CW processing does not use the cepstrum. This file includes the term only because
of the CMVN name.

## CMVN — Cepstral Mean and Variance Normalization

CMVN is a normalization step for each feature. Subtract the mean, then divide by
the standard deviation. Then the features have a mean of zero and a variance of
one. In speech recognition this removes a constant channel gain and level
differences. Thus the model gives the same result for loud input and for quiet
input.

The name contains "Cepstral" because the method started on cepstral and MFCC
features. Refer to MFCC above. But many systems apply the same normalization
directly to **STFT magnitude spectrograms**, and they keep the usual name. Two
variants are useful:

- **Per sample** — one mean and one standard deviation for the complete
  spectrogram. This removes the overall level of the sample.
- **Per bin** — normalize the time series of each frequency bin separately.
  Usually you do this on `log1p` magnitudes. This removes a sloped noise floor
  and gain differences between the bins. A detector for many signals needs this
  variant.

## Logit

A logit is a raw output score of a model. The model makes this value before it
changes the value into a probability. The name comes from statistics:
`logit(p) = log(p / (1 − p))`. This function is the inverse of the sigmoid
function and of the softmax function. It maps a probability in the range `(0, 1)`
to the range `(−∞, +∞)`.

In machine learning the word logits means the raw outputs of the last layer.
There is one real number for each class. A larger number shows a more probable
class. Apply the softmax function to the logits, and you get probabilities. The
sum of these probabilities is 1.

A neural CW decoder makes one logit for each vocabulary character, for each STFT
frame. A greedy decoder takes the argmax of the logits of each frame. It does not
calculate the softmax, because `argmax(logits) = argmax(softmax(logits))`. The
decoder needs only the index of the largest value.

## CTC — Connectionist Temporal Classification

CTC lets a model make one label for each timeframe, and still produce a shorter
text. The input and the output do not need an alignment frame by frame. The
vocabulary of a CTC model contains a special **blank** symbol. To change a
sequence of per-frame labels into text, apply two rules:

1. Replace each group of the same label with one label (`AAA` → `A`).
2. Delete all blanks.

The blank symbol keeps a true double character. `A blank A` becomes `AA`. But
`A A` becomes `A`. For Morse, the blank also shows the gaps between the elements
and between the characters.

A greedy CTC decoder takes the argmax of each frame, then applies the two rules.
Three variants are useful:

- **Plain** — apply the two rules and give clean text.
- **For display** — apply the same rules, but write a space where a blank or a
  group of the same label was. Then each output character keeps its position on
  the time axis. A visualization needs this.
- **With spans** — record the first frame and the last frame of each character,
  and of each gap between words. A streaming decoder needs these positions. It
  uses them to find a safe point where it can cut the text.

## SNR — Signal-to-Noise Ratio

The SNR shows how strong a wanted signal is in comparison to the background
noise. Almost always you give the value in **decibels**:
`SNR_dB = 10 · log10(signal_power / noise_power)`. An equivalent equation is
`20 · log10(signal_amplitude / noise_amplitude)`.

A positive value shows that the signal is above the noise floor. 0 dB shows that
the signal and the noise are equal. A negative value shows that the noise is
stronger than the signal. The value has no meaning without its reference
bandwidth. The same signal measured in 500 Hz and in 3 kHz gives two values that
differ by approximately 8 dB.

This is the classic method to estimate the SNR of one bin: subtract a local
noise-floor estimate from the amplitude of the bin. Calculate the noise floor as
a percentile or as a median of the adjacent bins.

The SNR controls the signal tracker. If you use only one threshold, a signal near
that threshold switches on and off many times. Use two thresholds and hysteresis.
The higher threshold starts a track at a new frequency. The lower threshold keeps
a track that exists.

## ACF — Autocorrelation Function

The ACF shows how much a signal is similar to a delayed copy of itself. The delay
has the name **lag**. The normalized ACF at the lag `k` is:

```
r(k) = Σ (x[i] − mean) · (x[i+k] − mean) / Σ (x[i] − mean)²
```

The range of `r(k)` is −1 to +1. A value of +1 shows that the signal repeats
itself exactly after `k` samples. A value of 0 shows no relation. A value of −1
shows that the signal is the opposite of itself: the marks of the copy are at the
position of the spaces of the original. The value at the lag 0 is always 1.

A periodic signal makes a peak at each multiple of its period. Thus the classic
use of the ACF is a pitch estimate or a period estimate: find the largest peak
after the lag 0.

The pipeline uses the ACF for a different purpose: it measures the **rate** of
the keying, and not the period. Refer to the CW discrimination in
[Research: CW Multi-Signal Decode Pipeline](./research_pipeline.md). The input is
the envelope of a signal that the tracker follows. It is one bit for each STFT
frame: the detection stage found a peak, or it did not.

`trackedSignal.minAutocorrelation` in
[the signal tracker](../pipeline/tracker_stage.go) takes the *smallest*
value over a range of lags, and not a peak. There are two reasons:

- **CW has no clean period.** The length of the symbols, of the characters and of
  the words is different. Thus a search for a peak has no stable target.
- **The smallest value measures the rate.** An envelope that switches faster than
  the lag range comes out of phase with itself somewhere in the range, so the
  value goes to 0 or below. An envelope that switches slower, or an envelope that
  does not switch, stays high at every lag of the range.

The lag range comes from the slowest keying rate that the system accepts. The lag
is the time of one element at that rate, thus one half of its period.

This is the important difference to the variance of the envelope, or to the duty
cycle: those measures show *if* a signal switches, but not *how fast*. A carrier
that switches on and off slowly has the duty cycle of CW, and only the ACF
removes it.

## Decodable frequency band

The decodable frequency band is the frequency window that the system looks at.
The system processes the signals inside the window. It ignores the signals
outside the window. The term has two related meanings:

- **A band for one signal, usually 400 Hz to 1200 Hz.** Audio-tone CW is usually
  between 400 Hz and 1000 Hz. Thus this band covers the useful range of tone
  pitches. It also removes the mains hum below and the hiss above. A neural
  decoder is trained on one band, and the width of its input is exactly the bins
  of that band. If a signal is at a different frequency, you must move it into the
  band first. Mix the signal, or shift the spectrogram.
- **A larger search range for detection.** Detection of many signals must see the
  complete spectrum, not only the band for one signal. Thus the peak search uses
  a larger window: 100 Hz to 2000 Hz for audio input, or the complete IQ
  bandwidth. Make the search range a parameter of the function. Do not connect it
  to the decode band.

## EMA — Exponential Moving Average

Also named **exponential filter**, or **one-pole IIR lowpass filter**. The three
names mean the same filter.

An EMA is a running average. It gives a larger weight to the recent samples. The
weight of an old sample decreases exponentially. The EMA needs no buffer of past
values. It needs only the last value and one calculation:

```
avg = α · new + (1 − α) · avg
```

An equivalent form is `avg = avg + α · (new − avg)`. Use this form in code,
because it shows the idea: move the output a fraction `α` of the distance to the
new input.

The factor `α` is between 0 and 1. It sets the speed of the response. A large `α`
gives a fast response, but the result is not steady. A small `α` gives a slow and
smooth result.

Use an EMA for each noisy value that you measure one time for each frame. These
are usual values: `α ≈ 0.08` for a noise floor or a spectrum estimate that must
stay steady, and `α ≈ 0.3` to track the frequency of a peak. Use a larger value
if the track must follow a fast change.

### Time constant

The value of `α` has no meaning without the sample rate. The **time constant**
`τ` is the better parameter, because it is a physical time. Use these equations
to convert between `α` and `τ`:

```
α = 1 − exp(−T / τ)        T = 1 / sample_rate
τ = −T / ln(1 − α)

α ≈ T / τ                  if τ is much larger than T
```

The step response gives `τ` its meaning. The output goes to 63 % of a step after
one `τ`. It goes to 95 % after 3 `τ`. The equivalent cutoff frequency is
`f_c = 1 / (2π · τ)`.

Example: a noise floor estimate uses `τ = 2 s`. The STFT makes one frame each
21.3 ms, so `T = 0.0213 s`. Then `α = 1 − exp(−0.0213 / 2) = 0.011`.

Always specify the filter by `τ`, not by `α`. If you change the hop or the
decimation factor, `T` changes, and `α` must change with it. A constant value of
`α` in the code gives a different filter after such a change, and it gives no
error message.
