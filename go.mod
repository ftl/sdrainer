module github.com/ftl/sdrainer

go 1.22

toolchain go1.23.4

// replace github.com/ftl/digimodes => ../digimodes

// replace github.com/ftl/patrix => ../patrix

// replace github.com/ftl/tci => ../tci

// replace github.com/ftl/hamradio => ../hamradio

require (
	github.com/ftl/digimodes v0.0.0-20231231131023-cffadad68e9e
	github.com/ftl/hamradio v0.2.9
	github.com/ftl/tci v0.3.2
	github.com/golang/protobuf v1.5.4
	github.com/gorilla/websocket v1.5.1
	github.com/jfreymuth/pulse v0.1.0
	github.com/mjibson/go-dsp v0.0.0-20180508042940-11479a337f12
	github.com/spf13/cobra v1.8.0
	github.com/stretchr/testify v1.8.4
	golang.org/x/exp v0.0.0-20231226003508-02704c960a9b
	google.golang.org/grpc v1.70.0
	google.golang.org/protobuf v1.36.4
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/ftl/localcopy v0.0.0-20190616142648-8915fb81f0d9 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/texttheater/golang-levenshtein v1.0.1 // indirect
	golang.org/x/net v0.32.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20241202173237-19429a94021a // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
