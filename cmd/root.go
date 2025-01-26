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

	"github.com/spf13/cobra"

	"github.com/ftl/sdrainer/scope"
)

var (
	version   string = "develop"
	gitCommit string = "-"
	buildTime string = "-"
)

var rootFlags = struct {
	pprof        bool
	debug        bool
	scope        bool
	scopeAddress string
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
	rootCmd.PersistentFlags().BoolVar(&rootFlags.debug, "debug", false, "enable debug logging")
	rootCmd.PersistentFlags().BoolVar(&rootFlags.scope, "scope", false, "enable the scope server for insights into the inner workings")
	rootCmd.PersistentFlags().StringVar(&rootFlags.scopeAddress, "scope-address", ":35369", "listening address and scope for the scope server")

	rootCmd.PersistentFlags().MarkHidden("pprof")
	rootCmd.PersistentFlags().MarkHidden("scope")
	rootCmd.PersistentFlags().MarkHidden("scope_address")
}

func runWithCtx(f func(ctx context.Context, scope scope.Scope, cmd *cobra.Command, args []string)) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
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

		var scopeServer *scope.ScopeServer
		var s scope.Scope = scope.NewNullScope()
		if rootFlags.scope {
			scopeServer = scope.NewScopeServer(rootFlags.scopeAddress)
			err := scopeServer.Start()
			if err != nil {
				log.Fatalf("cannot start scope server: %v", err)
			}
			s = scopeServer
		}

		ctx, cancel := context.WithCancel(context.Background())
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
		go handleCancelation(signals, cancel)

		f(ctx, s, cmd, args)

		if scopeServer != nil {
			scopeServer.Stop()
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
