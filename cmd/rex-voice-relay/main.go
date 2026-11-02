// rex-voice-relay pairs the phone and the Mac by id and forwards encrypted frames.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yang1997434/rex-voice/internal/relay"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8790", "listen address")
	flag.Parse()

	srv := &http.Server{Addr: *listen, Handler: relay.NewHub(), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("listening on %s", ln.Addr())
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		log.Fatalf("serve: %v", err)
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		log.Fatalf("shutdown: %v", err)
	}
	log.Print("stopped")
}
