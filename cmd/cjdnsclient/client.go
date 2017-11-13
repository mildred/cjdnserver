package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/fc00/go-cjdns/key"
	"github.com/mildred/cjdnserver"
	"github.com/mildred/simpleipc"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

func main() {
	var sockPath string
	var watchdog bool
	var privkey string
	flag.StringVar(&sockPath, "sock", "/run/cjdnserver/cjdserver.sock", "Socker file path")
	flag.BoolVar(&watchdog, "watchdog", false, "internal use")
	flag.StringVar(&privkey, "privkey", "", "private key")
	flag.Parse()

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	cjdnserver.CancelSignals(ctx, &wg, cancel, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if watchdog {
		f := os.NewFile(3, "socket")
		c, err := net.FileConn(f)
		if err != nil {
			log.Fatal(err)
		}
		err = runWatchdog(ctx, &wg, c.(*net.UnixConn))
		if err != nil {
			log.Fatal(err)
		}
	} else {
		err := run(ctx, &wg, sockPath, privkey)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func run(ctx context.Context, wg *sync.WaitGroup, sockPath, privkey string) error {
	var err error
	var skey *key.Private = nil

	if privkey != "" {
		skey, err = key.DecodePrivate(privkey)
		if err != nil {
			return err
		} else if !skey.Valid() {
			return fmt.Errorf("invalid private key")
		}
	}

	cnx0, err := net.Dial("unix", sockPath)
	if err != nil {
		return err
	}

	netns, err := os.OpenFile("/proc/self/ns/net", os.O_RDONLY, 0)
	if err != nil {
		return err
	}

	cnx := cnx0.(*net.UnixConn)

	log.Printf("Connected to server %v", cnx)
	h := simpleipc.NewHeader(cjdnserver.InitialRequest, 0, []*os.File{netns})
	if skey != nil {
		h.Size = uint32(len(*skey))
	}
	if skey != nil {
		err = h.WriteWithPayload(cnx, skey[:])
	} else {
		err = h.WriteWithPayload(cnx, nil)
	}
	if err != nil {
		return err
	}

	f, err := cnx.File()
	if err != nil {
		return err
	}

	err = h.Read(cnx, nil)
	if err != nil {
		return err
	}

	cmd := exec.Command(os.Args[0], "-watchdog")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.ExtraFiles = append(cmd.ExtraFiles, f)
	err = cmd.Start()
	if err != nil {
		return err
	}

	return nil
}

func runWatchdog(ctx context.Context, wg *sync.WaitGroup, cnx *net.UnixConn) error {
	var err error

	log.Printf("Running watchdog")
	h := simpleipc.NewHeader(cjdnserver.WatchdogPing, 0, []*os.File{})
	for ctx.Err() == nil {
		timeout, _ := context.WithTimeout(ctx, 30*time.Second)
		<-timeout.Done()
		err = h.Write(cnx)
		if err != nil {
			return err
		}
	}

	log.Printf("Stopped watchdog")
	return nil
}
