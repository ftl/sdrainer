# SDRainer

Combine a pasta strainer with a **S**oftware **D**efined **R**adio and you get a **SDRainer**. It separates all the tasty CW signals from the ether:

- decode a CW signal from a Pulseaudio source
- find and decode CW signals in an IQ stream coming in through the TCI protocol
- show the spotted callsigns as spots on the TCI device's spectrum display
- provide access to spotted callsigns through a telnet connection, like a local DX cluster

This is work in progress, the CW decoder is still a bit inaccurate for weak signals.

## Usage

Decode a CW signal from a Pulseaudio source:
```
sdrainer decode pulse
```

Decode a CW signal at the VFO A frequency of a TCI device:
```
sdrainer decode tci
```

Detect and collect callsigns from a TCI device's IQ stream:
```
sdrainer strain tci
```

Use `sdrainer --help` or `sdrainer <cmd> <sub-cmd> --help` to find out more information about the supported parameters for each command and sub-command.

## Planned Features

- send spotted callsigns to a dx cluster
- add support for other SDR devices (KiwiSDR, HDSDR, SDRplay, RTL-SDR, IC-7610, FlexRadio)
- add support for other digital modes (PSK31, RTTY)

## Sponsors

I started this just to scratch the itch of learning how a CW skimmer might work. If you like what I'm doing here and want to support the further development, please consider becoming a [sponsor of this project](https://github.com/sponsors/ftl).

## License

This software is published under the [MIT License](https://www.tldrlegal.com/l/mit).

Copyright [Florian Thienel](http://thecodingflow.com/)