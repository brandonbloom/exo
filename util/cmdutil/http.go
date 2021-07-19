package cmdutil

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"
)

func ListenAndServe(ctx context.Context, svr *http.Server) {
	go svr.ListenAndServe()
	ShutdownOnInterrupt(ctx, svr)
}

func Serve(ctx context.Context, l net.Listener, svr *http.Server) {
	go svr.Serve(l)
	ShutdownOnInterrupt(ctx, svr)
}

func ShutdownOnInterrupt(ctx context.Context, svr *http.Server) {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	<-ctx.Done()
	stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = svr.Shutdown(ctx)
}