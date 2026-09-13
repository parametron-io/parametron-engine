package api

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/handoff"
	"strings"
	"time"
)

const (
	DefaultAddr = ":8080"
	Version     = "dev"
)

type Config struct {
	Addr        string
	ArtifactDir string
	Version     bool
	Help        bool
}

type HandlerOptions struct {
	ArtifactStore   artifact.Store
	SubmissionStore SubmissionStore
	JobQueue        JobQueue
	RunRoot         string
	WorkerCount     int
	PollInterval    time.Duration
	ExecutorFactory func(*handoff.Package) packageExecutor
}

type Server struct {
	Addr            string
	Output          io.Writer
	ShutdownTimeout time.Duration
	Handler         http.Handler
	onListen        func(net.Addr)
	listen          func(network, addr string) (net.Listener, error)
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	_ = stderr

	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}

	if cfg.Help {
		writeUsage(stdout)
		return nil
	}

	if cfg.Version {
		_, err := fmt.Fprintf(stdout, "parametron-engine version %s\n", Version)
		return err
	}

	var handler http.Handler
	if strings.TrimSpace(cfg.ArtifactDir) != "" {
		handler = newHandler(HandlerOptions{
			ArtifactStore: artifact.NewFileSystemStore(cfg.ArtifactDir),
		})
	}

	server := Server{
		Addr:    cfg.Addr,
		Output:  stdout,
		Handler: handler,
	}

	return server.Run(ctx)
}

func parseConfig(args []string) (Config, error) {
	cfg := Config{Addr: DefaultAddr}

	flags := flag.NewFlagSet("parametron-engine", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&cfg.Help, "help", false, "Print help and exit")
	flags.BoolVar(&cfg.Version, "version", false, "Print version and exit")
	flags.StringVar(&cfg.Addr, "addr", DefaultAddr, "Listen address")
	flags.StringVar(&cfg.ArtifactDir, "artifact-dir", "", "Artifact store base directory")

	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}

	if flags.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}

	if strings.TrimSpace(cfg.Addr) == "" {
		return Config{}, fmt.Errorf("addr must not be empty")
	}
	if cfg.ArtifactDir != "" && strings.TrimSpace(cfg.ArtifactDir) == "" {
		return Config{}, fmt.Errorf("artifact-dir must not be empty")
	}

	return cfg, nil
}

func writeUsage(w io.Writer) {
	if w == nil {
		return
	}

	fmt.Fprintln(w, "Usage of parametron-engine:")
	fmt.Fprintln(w, "  parametron-engine [--addr <listen-address>] [--artifact-dir <path>] [--version] [--help]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintf(w, "  --addr string\n    \tListen address (default %q)\n", DefaultAddr)
	fmt.Fprintln(w, "  --artifact-dir string")
	fmt.Fprintln(w, "    \tArtifact store base directory (enables artifact-serving and job artifact visibility)")
	fmt.Fprintln(w, "  --help")
	fmt.Fprintln(w, "    \tPrint help and exit")
	fmt.Fprintln(w, "  --version")
	fmt.Fprintln(w, "    \tPrint version and exit")
}

func (s Server) Run(ctx context.Context) error {
	addr := s.Addr
	if addr == "" {
		addr = DefaultAddr
	}

	output := s.Output
	if output == nil {
		output = io.Discard
	}

	listen := s.listen
	if listen == nil {
		listen = net.Listen
	}

	listener, err := listen("tcp", addr)
	if err != nil {
		return err
	}

	if s.onListen != nil {
		s.onListen(listener.Addr())
	}

	handler := s.Handler
	if handler == nil {
		handler = newHandler()
	}
	var runtimeHandler handlerRuntime
	if startedRuntimeHandler, ok := handler.(handlerRuntime); ok {
		runtimeHandler = startedRuntimeHandler
		runtimeHandler.Start(ctx)
	}

	shutdownTimeout := s.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = 5 * time.Second
	}

	httpServer := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	fmt.Fprintf(output, "parametron-engine listening on %s\n", listener.Addr().String())

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	err = httpServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownDone
		if runtimeHandler != nil {
			runtimeHandler.Wait()
		}
		return nil
	}

	return err
}

func newHandler(options ...HandlerOptions) http.Handler {
	var opts HandlerOptions
	if len(options) > 0 {
		opts = options[0]
	}
	if opts.SubmissionStore == nil {
		opts.SubmissionStore = newInMemorySubmissionStore()
	}
	if opts.JobQueue == nil {
		opts.JobQueue = newInMemoryJobQueue()
	}
	if root, err := resolveRuntimeRoot(opts.ArtifactStore, opts.RunRoot); err == nil {
		opts.RunRoot = root
	}
	if opts.ExecutorFactory == nil {
		factory, err := defaultExecutorFactory(opts.ArtifactStore, opts.RunRoot)
		if err == nil {
			opts.ExecutorFactory = factory
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.Handle("/job", newJobSubmissionHandler(opts.SubmissionStore, opts.JobQueue))
	mux.Handle("/job/", newJobScopedHandler(opts.SubmissionStore, opts.ArtifactStore))

	if opts.ArtifactStore != nil {
		resolver := artifact.NewResolver(opts.ArtifactStore)
		mux.Handle("/artifacts", artifact.NewListHandler(resolver))
		mux.Handle("/artifacts/", artifact.NewFileHandler("/artifacts/", resolver))
	}

	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/job/") {
			if _, _, ok := jobRouteFromPath(r.URL.Path); !ok {
				http.NotFound(w, r)
				return
			}
		}

		mux.ServeHTTP(w, r)
	})

	return &runtimeAwareHandler{
		base:   base,
		runner: newJobRunner(opts.SubmissionStore, opts.ArtifactStore, opts.RunRoot, opts.JobQueue, opts.ExecutorFactory, opts.WorkerCount, opts.PollInterval),
	}
}
