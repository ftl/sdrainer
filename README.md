# SDRainer

Combine a pasta strainer with a **S**oftware **D**efined **R**adio and you get a **SDRainer**. It separates all the tasty CW signals from the ether:

- decode a CW signal from a Pulseaudio source
- find and decode CW signals in an IQ stream coming in through the TCI protocol
- show the spotted callsigns as spots on the TCI device's spectrum display

This is work in progress, the CW decoder is still very inaccurate and cannot handle varying speed or signal strength.

## Planned Features

- provide access to spotted callsigns through a telnet connection
- send spotted callsigns to a dx cluster
- add support for other SDR devices (HDSDR, SDRplay, RTL-SDR, IC-7610)
- add support for other digital modes (PSK31, RTTY)

## License

This software is published under the [MIT License](https://www.tldrlegal.com/l/mit).

Copyright [Florian Thienel](http://thecodingflow.com/)