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

	hub := relay.NewHub()
	invites, err := relay.ParseInvites(os.Getenv("RELAY_INVITES"))
	if err != nil {
		log.Fatalf("RELAY_INVITES: %v", err) // refuse to start open when the operator meant to lock it
	}
	hub.Invites = invites
	if len(invites) == 0 {
		log.Print("RELAY_INVITES not set, anyone can connect")
	} else {
		log.Printf("invites required (%d codes)", len(invites))
	}
	keyPath, keyID, teamID := os.Getenv("APNS_KEY_PATH"), os.Getenv("APNS_KEY_ID"), os.Getenv("APNS_TEAM_ID")
	if keyPath == "" || keyID == "" || teamID == "" {
		log.Print("warn: APNS_KEY_PATH / APNS_KEY_ID / APNS_TEAM_ID not all set, push disabled")
	} else {
		// a bad key only disables push; the relay itself must stay up for calls
		if n, err := relay.NewAPNS(keyPath, keyID, teamID, os.Getenv("APNS_URL")); err != nil {
			log.Printf("warn: apns disabled: %v", err)
		} else {
			hub.Notifier = n
			log.Printf("apns push enabled key=%s team=%s", keyID, teamID)
		}
	}

	srv := &http.Server{Addr: *listen, Handler: hub, ReadHeaderTimeout: 10 * time.Second}
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
