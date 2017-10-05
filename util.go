package cjdnserver

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
)

func CancelSignals(cancelContext context.CancelFunc, wg *sync.WaitGroup, signals ...os.Signal) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, signals...)
	wg.Add(1)
	go func() {
		defer wg.Done()
		s := <-signalChan
		log.Printf("Captured %v. Exiting...", s)
		cancelContext()
		signal.Reset(signals...)
	}()
}
