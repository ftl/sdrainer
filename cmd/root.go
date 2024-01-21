package cmd

import (
	"context"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/ftl/sdrainer/trace"
)

var (
	version   string = "develop"
	gitCommit string = "-"
	buildTime string = "-"
)

var rootFlags = struct {
	pprof            bool
	traceContext     string
	traceDestination string
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
	rootCmd.PersistentFlags().BoolVar(&rootFlags.pprof, "pprof", false, "enable pprof")
	rootCmd.PersistentFlags().StringVar(&rootFlags.traceContext, "trace", "", "spectrum | demod | decode")
	rootCmd.PersistentFlags().StringVar(&rootFlags.traceDestination, "trace_to", "", "file:<filename> | udp:<host:port>")

	rootCmd.PersistentFlags().MarkHidden("pprof")
	rootCmd.PersistentFlags().MarkHidden("trace")
	rootCmd.PersistentFlags().MarkHidden("trace_to")
}

func runWithCtx(f func(ctx context.Context, cmd *cobra.Command, args []string)) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		if rootFlags.pprof {
			go func() {
				log.Printf("starting pprof on http://localhost:6060/debug/pprof")
				log.Println(http.ListenAndServe("localhost:6060", nil))
			}()
		}

		ctx, cancel := context.WithCancel(context.Background())
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
		go handleCancelation(signals, cancel)

		f(ctx, cmd, args)
	}
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

func createTracer() (trace.Tracer, bool) {
	if rootFlags.traceDestination == "" {
		log.Printf("no destination")
		return nil, false
	}

	protocol, destination, found := strings.Cut(rootFlags.traceDestination, ":")
	if !found {
		log.Printf("wrong parts %v", rootFlags.traceDestination)
		return nil, false
	}

	switch strings.ToLower(protocol) {
	case "file":
		return trace.NewFileTracer(rootFlags.traceContext, destination), true
	case "udp":
		return trace.NewUDPTracer(rootFlags.traceContext, destination), true
	default:
		log.Printf("wrong protocol %v", protocol)
		return nil, false
	}
}
