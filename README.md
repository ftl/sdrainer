# SDRainer

Combine a pasta strainer with a **S**oftware **D**efined **R**adio and you get a **SDRainer**. It separates all the tasty CW signals from the ether:

- find and decode CW signals in an IQ stream coming from a variety of sources
- show the spotted callsigns as spots on the TCI device's spectrum display
- provide access to spotted callsigns through a telnet connection, like a local DX cluster

**This project is experimental and incomplete, take everything with a grain of salt**

## Usage

Detect and collect callsigns from a TCI device's IQ stream on the default host (`localhost:40001`):
```
sdrainer tci --cluster --show-spots
```

Detect and collect callsigns from a KiwiSDR's IQ stream:
```
sdrainer kiwi --host=some.kiwi.host.net:8073 --cluster
```

Use `sdrainer --help` or `sdrainer <cmd> --help` to find out more information about the supported parameters for each command and sub-command.

## Planned Features

- send spotted callsigns to a dx cluster
- add support for other SDR devices (RTL-SDR)
- add support for other digital modes (PSK31, RTTY)

## License

This software is published under the [MIT License](https://www.tldrlegal.com/l/mit).

Copyright [Florian Thienel](http://thecodingflow.com/)

It uses third party components with their own licenses, see the file
[NOTICE](./NOTICE).
