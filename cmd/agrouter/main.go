// agrouter is an experimental, extension-free Cloud Code bridge.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"multiAG/internal/routerbridge"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:20129", "loopback listen address")
	router := flag.String("router", "http://127.0.0.1:20128", "9Router base URL")
	upstream := flag.String("upstream", "https://cloudcode-pa.googleapis.com", "Google metadata endpoint")
	model := flag.String("model", "", "optional fixed router model")
	wireFormat := flag.String("wire-format", "native", "router request format: native or openai")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		fmt.Fprintln(os.Stderr, "listen must use a loopback IP")
		os.Exit(1)
	}
	b, err := routerbridge.New(routerbridge.Options{RouterURL: *router, UpstreamURL: *upstream, APIKey: os.Getenv("AG_ROUTER_API_KEY"), Capability: os.Getenv("AG_ROUTER_CAPABILITY"), Model: *model, WireFormat: *wireFormat})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	srv := &http.Server{Addr: *listen, Handler: b, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	fmt.Println("Antigravity router bridge listening on", *listen)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "bridge listener failed")
		os.Exit(1)
	}
	<-shutdownDone
}
