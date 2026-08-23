package cmd

import (
	"context"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/ftl/sdrainer/cluster"
	"github.com/ftl/sdrainer/core"
	"github.com/ftl/sdrainer/dsp"
	"github.com/ftl/sdrainer/iq"
	"github.com/ftl/sdrainer/scope"
)

var (
	version   string = "develop"
	gitCommit string = "-"
	buildTime string = "-"
)

var rootFlags = struct {
	pprof bool
	debug bool

	service        bool
	serviceAddress string
	scope          bool

	cluster           bool
	clusterAddress    string
	clusterCall       string
	spotSilencePeriod time.Duration
	record            string
}{}

var rootCmd = &cobra.Command{
	Use:   "sdrainer",
	Short: "SDRainer - combine a pasta strainer with an SDR...",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&rootFlags.cluster, "cluster", false, "enable the DX cluster telnet server")
	rootCmd.PersistentFlags().StringVar(&rootFlags.clusterAddress, "cluster-address", ":7373", "the binding address and port for the DX cluster")
	rootCmd.PersistentFlags().StringVar(&rootFlags.clusterCall, "cluster-call", "local-#", "the reporter's callsign of the cluster spots")
	rootCmd.PersistentFlags().DurationVar(&rootFlags.spotSilencePeriod, "spot-every", 1*time.Minute, "the time period after an active callsign is spotted again")

	rootCmd.PersistentFlags().BoolVar(&rootFlags.service, "service", false, "enable the gRPC server")
	rootCmd.PersistentFlags().StringVar(&rootFlags.serviceAddress, "service-address", ":35369", "binding address and port for the gRPC server")
	rootCmd.PersistentFlags().BoolVar(&rootFlags.scope, "scope", false, "enable the scope gRPC service for insights into the inner workings")

	rootCmd.PersistentFlags().StringVar(&rootFlags.record, "record", "", "record the IQ stream into this file")

	rootCmd.PersistentFlags().BoolVar(&rootFlags.pprof, "pprof", false, "enable pprof")
	rootCmd.PersistentFlags().BoolVar(&rootFlags.debug, "debug", false, "enable debug logging")

	rootCmd.PersistentFlags().MarkHidden("pprof")
	rootCmd.PersistentFlags().MarkHidden("debug")
}

func runPipeline[F dsp.Number](f func(context.Context, core.ScopeService, core.ChannelService[F], core.Spotter[F], *iq.Writer, *cobra.Command, []string)) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		var err error

		if !rootFlags.debug {
			log.SetOutput(&nopWriter{})
		}

		log.Printf("SDRainer Version %s", formatVersion())

		if rootFlags.pprof {
			go func() {
				log.Printf("starting pprof on http://localhost:6060/debug/pprof")
				log.Println(http.ListenAndServe("localhost:6060", nil))
			}()
		}

		var grpcServer *scope.ScopeServer[F]
		var scopeService core.ScopeService = &core.NullScopeService{}
		var channelService core.ChannelService[F] = &core.NullChannelService[F]{}
		if rootFlags.service {
			grpcServer = scope.NewScopeServer[F](rootFlags.serviceAddress)
			err = grpcServer.Start()
			if err != nil {
				log.Fatalf("cannot start gRPC server: %v", err)
			}
			if rootFlags.scope {
				scopeService = grpcServer
			}
			channelService = grpcServer
		}

		var clusterServer *cluster.Server[F]
		var spotter core.Spotter[F]
		spotter = &core.NullSpotter[F]{}
		if rootFlags.cluster {
			clusterServer, err = cluster.NewServer[F](rootFlags.clusterAddress, rootFlags.clusterCall, formatVersion())
			if err != nil {
				log.Fatalf("cannot start DX cluster server: %v", err)
			}
			clusterServer.SetSilencePeriod(rootFlags.spotSilencePeriod)
			spotter = clusterServer
		}

		var recorder *iq.Writer
		if rootFlags.record != "" {
			recorder, err = iq.NewWriter(rootFlags.record)
			if err != nil {
				log.Fatalf("cannot record the IQ stream: %v", err)
			}
			log.Printf("recording the IQ stream into %s", recorder.Name())
		}

		ctx, cancel := context.WithCancel(context.Background())
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
		go handleCancelation(signals, cancel)

		f(ctx, scopeService, channelService, spotter, recorder, cmd, args)

		if recorder != nil {
			if err := recorder.Close(); err != nil {
				log.Printf("cannot close the IQ file: %v", err)
			}
		}

		if clusterServer != nil {
			clusterServer.Stop()
		}

		if grpcServer != nil {
			grpcServer.Stop()
		}
	}
}

func formatVersion() string {
	if gitCommit == "-" && buildTime == "-" {
		return version
	}
	return fmt.Sprintf("%s_%s_%s", version, gitCommit, buildTime)
}

func handleCancelation(signals <-chan os.Signal, cancel context.CancelFunc) {
	count := 0
	for range signals {
		count++
		if count == 1 {
			cancel()
		} else {
			log.Fatal("hard shutdown")
		}
	}
}

type nopWriter struct{}

func (w *nopWriter) Write(p []byte) (n int, err error) { return len(p), nil }
