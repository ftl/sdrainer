module github.com/ftl/sdrainer

go 1.20

replace github.com/ftl/digimodes => ../digimodes

replace github.com/ftl/patrix => ../patrix

require (
	github.com/ftl/digimodes v0.0.0-20200502133046-0a4117101b05
	github.com/ftl/patrix v0.0.0-20231216163204-d2f8d83f211b
	github.com/jfreymuth/pulse v0.1.0
	github.com/spf13/cobra v1.8.0
	github.com/stretchr/testify v1.8.4
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
