# Architecture Goal

The current state of SDRainer is highly experimental with man incomplete parts. This document describes the goal we are working towards, in contrast to [Architecture Overview](./architecture_overview.md), which shows the current state. 

This document will iteratively be refined and extended to cover all relevant parts of the application to a certain level of detail, with focus on the interaction between the different components. For deeper insights about the application's components, there will be separate documents. 

## Overview

SDRainer is a CW (Morse Code) skimmer: it detects and decodes CW signals from an SDR IQ stream, extracts callsigns, and publishes them as spots using established protocols, for example a telnet DX-cluster interface, or TCI spectrum markers. 

SDRainer supports several SDRs with a network interface for ingesting the IQ data:
- TCI (ExpertSDR, AetherSDR, Thetis)
- OpenHPSDR (Hermes-Lite 2)
- `rtl_tcp`
- Icom RS-BA1

Ingesting IQ data over USB is currently out of scope.

## Runtime

- headless server application
- available on multiple platforms (linux, windows, mac, x86, arm, as docker container, standalone)
- application log on stdout/stderr
- configuration via YAML file and environment variables
- provides data that can be used for visualisation through gRPC streaming endpoints

## Interfaces

- Input: serveral different SDR clients (TCI, OpenHPSDR, rtl_tcp, Icom RS-BA1)
- Output:
  - telnet DX-cluster server
  - TCI spectrum markers (only, if the input is also TCI)
  - gRPC stream of decoded CW and channel information (see [pipeline](#pipeline))
- Visualization: gRPC stream of spectral frames (see [pipeline](#pipeline))

## Pipeline

See [Research: CW Multi-Signal Decode Pipeline](./research_pipeline.md) for theoretical foundation.

- generic implementation with `S` = sample data type and `F` = frequency data type, both based on `dsp.Number`
- no hard values for sample rate, bandwidths, thresholds etc., everything is configurable through one configuration struct
- consists of stages that are linked by channels

## Glossary

**Spectral Frame**: data structure for transport of intermediate processing result through the DSP stages; starts with a chunk of IQ data, collects for example the FFT, detected peaks, detected noise floor; is recycled after use instead of abandoned and garbage collected

**Channel**: data structure that represents a tracked CW signal on a certain frequency, streaming the decoded CW, collecting metadata (callsigns, first seen, last seen, ...)
