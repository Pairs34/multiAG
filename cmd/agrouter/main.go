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
	stall := flag.Duration("stall-timeout", 2*time.Minute, "abort a router request that sends no bytes for this long")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		fmt.Fprintln(os.Stderr, "listen must use a loopback IP")
		os.Exit(1)
	}
	var debugRaw, debugFrames *os.File
	if dir := os.Getenv("AG_ROUTER_DEBUG_DIR"); dir != "" {
		debugRaw, err = os.OpenFile(dir+string(os.PathSeparator)+"router-raw.log", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
		if err == nil {
			debugFrames, err = os.OpenFile(dir+string(os.PathSeparator)+"ide-frames.log", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "cannot open debug logs:", err)
			os.Exit(1)
		}
	}
	options := routerbridge.Options{RouterURL: *router, UpstreamURL: *upstream, APIKey: os.Getenv("AG_ROUTER_API_KEY"), Capability: os.Getenv("AG_ROUTER_CAPABILITY"), Model: *model, WireFormat: *wireFormat, StallTimeout: *stall}
	if debugRaw != nil {
		options.DebugRaw, options.DebugFrames = debugRaw, debugFrames
	}
	b, err := routerbridge.New(options)
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
