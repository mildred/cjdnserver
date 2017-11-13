package cjdnserver

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
)

func CancelSignals(ctx context.Context, wg *sync.WaitGroup, cancelContext context.CancelFunc, signals ...os.Signal) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, signals...)
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case s := <-signalChan:
			log.Printf("Captured %v. Exiting...", s)
			cancelContext()
		case <-ctx.Done():
		}
		signal.Reset(signals...)
	}()
}
