package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/fc00/go-cjdns/admin"
	"github.com/fc00/go-cjdns/key"
	"github.com/jbenet/go-reuseport"
	"github.com/mildred/cjdnserver"
	"github.com/mildred/simpleipc"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	InterfaceMTU = 1304
)

type Peer struct {
	Address  string
	Password string
	Pubkey   string
}

func main() {
	var peer Peer
	var sockPath string
	var perms string
	var cjdroute string
	flag.StringVar(&sockPath, "sock", "/run/cjdnserver/cjdserver.sock", "Socket file path")
	flag.StringVar(&perms, "perms", "0755", "Socket permissions")
	flag.StringVar(&cjdroute, "cjdroute", "cjdroute", "cjdroute executable")
	flag.StringVar(&peer.Address, "peer-address", "", "Peer address to connect to over UDP")
	flag.StringVar(&peer.Password, "peer-password", "", "Peer password")
	flag.StringVar(&peer.Pubkey, "peer-pubkey", "", "Peer public key")
	flag.Parse()

	perms1, _ := strconv.ParseInt(perms, 8, 32)

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	cjdnserver.CancelSignals(cancel, &wg, syscall.SIGINT, syscall.SIGTERM)

	err := run(ctx, &wg, cjdroute, sockPath, os.FileMode(perms1), &peer)
	if err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, wg *sync.WaitGroup, cjdroute, sockPath string, perms os.FileMode, peer *Peer) error {
	var l net.Listener
	err := os.MkdirAll(path.Dir(sockPath), 0755)
	if err != nil {
		return err
	}
	_, err = os.Stat(sockPath)
	if err == nil {
		log.Printf("Remove %s", sockPath)
		err = os.Remove(sockPath)
		if err != nil {
			return err
		}
	}
	log.Printf("Listen on %s", sockPath)
	l, err = net.Listen("unix", sockPath)
	if err != nil {
		return err
	}
	defer l.Close()

	go func() {
		<-ctx.Done()
		l.Close()
	}()

	log.Printf("Chmod %s to 0%s", sockPath, strconv.FormatInt(int64(perms), 8))
	err = os.Chmod(sockPath, perms)
	if err != nil {
		log.Print(err)
	}

	for ctx.Err() == nil {
		cnx, err := l.Accept()
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}
		wg.Add(1)
		go (func() {
			defer wg.Done()
			err := handleClient(ctx, wg, cnx.(*net.UnixConn), cjdroute, peer)
			if err != nil {
				log.Print(err)
			}
		})()
	}

	return nil
}

func parseAdminAddr(addr string) (string, int) {
	i := strings.Index(addr, ":")
	port, _ := strconv.ParseInt(addr[i+1:], 10, 32)
	return addr[:i], int(port)
}

func handleClient(ctx0 context.Context, wg *sync.WaitGroup, cnx *net.UnixConn, cjdroute string, peer *Peer) error {
	ctx, cancel := context.WithCancel(ctx0)

	adminif, err := reuseport.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	adminaddr := adminif.LocalAddr().String()
	log.Printf("Listen to admin %v", adminaddr)
	var adminConf admin.CjdnsAdminConfig
	adminConf.Addr, adminConf.Port = parseAdminAddr(adminaddr)
	defer adminif.Close()

	h := new(simpleipc.Header)
	privkey, err := h.ReadWithPayload(cnx, nil)
	if err != nil {
		return err
	}
	var skey *key.Private
	if len(privkey) > 0 {
		skey = new(key.Private)
		if len(privkey) == len(*skey) {
			copy(skey[:], privkey)
		} else {
			skey = nil
		}
	}
	log.Printf("Received header %#v", h)
	if len(h.Files) == 0 {
		return fmt.Errorf("Did not received any file descriptor")
	}

	suffix := ""
	if skey != nil {
		suffix = "-" + skey.Pubkey().IP().String()
	}
	tmpdir, err := ioutil.TempDir("", "cjdnserver-client"+suffix)
	if err != nil {
		return err
	}
	log.Printf("Receive client in %v", tmpdir)
	defer os.RemoveAll(tmpdir)

	sockpath := path.Join(tmpdir, "cjdnstun.socket")
	conf, ipv6, adminpass, err := Genconf(cjdroute, sockpath, adminaddr, peer, skey)
	adminConf.Password = adminpass
	if err != nil {
		return err
	}

	conffile := path.Join(tmpdir, "cjdroute.conf")
	err = ioutil.WriteFile(conffile, []byte(conf), 0644)
	if err != nil {
		return err
	}

	log.Printf("Admin interface ip %s port %d password %#v", adminConf.Addr, adminConf.Port, adminConf.Password)
	log.Printf("Configuration file written to %s", conffile)
	log.Print(conf)

	tunfd, err := MakeTunInNs(h.Files[0], ipv6, InterfaceMTU)
	if err != nil {
		return err
	}
	defer tunfd.Close()

	wg.Add(1)
	go func() {
		defer wg.Done()
		receiveWatchdog(ctx, wg, cancel, cnx)
	}()

	for ctx.Err() == nil {
		log.Printf("Start cjdroute")
		process, err := Start(cjdroute, conf)
		if err != nil {
			return err
		}

		go (func() {
			err := SendTunDev(sockpath, tunfd)
			if err != nil {
				log.Print(err)
			}
		})()

		h = simpleipc.NewHeader(cjdnserver.InitialResponse, 0, nil)
		err = h.Write(cnx)
		if err != nil {
			return err
		}

		cstate := make(chan *os.ProcessState)
		cerr := make(chan error)
		wg.Add(1)
		go func() {
			defer wg.Done()
			state, err := process.Wait()
			if err != nil {
				cerr <- err
			} else {
				cstate <- state
			}
		}()

		select {
		case <-ctx.Done():
			log.Printf("Send Core_exit()")
			adm, err := admin.Connect(&adminConf)
			if err != nil {
				return fmt.Errorf("connect to admin interface: %v", err)
			}
			err = adm.Core_exit()
			if err != nil {
				log.Printf("Core_exit: %v", err)
			}
			log.Printf("Send SIGTERM to cjdroute")
			process.Signal(syscall.SIGTERM)
		case err := <-cerr:
			log.Printf("Error: %s", err)
		case state := <-cstate:
			log.Printf("Terminated: %s", state.String())
		}
	}

	log.Printf("Stopped %s", ipv6)

	return nil
}

func receiveWatchdog(ctx0 context.Context, wg *sync.WaitGroup, cancel context.CancelFunc, cnx *net.UnixConn) {
	ctx, cancel2 := context.WithCancel(ctx0)
	ping := make(chan struct{})
	//wg.Add(1)
	go func() {
		//defer wg.Done()
		for ctx.Err() == nil {
			h := new(simpleipc.Header)
			_, err := h.ReadWithPayload(cnx, nil)
			if err != nil {
				log.Printf("Received error: %s", err)
				log.Printf("Watchdog triggered stop")
				cancel()
				cancel2()
			} else if h.Seq == cjdnserver.WatchdogPing {
				ping <- struct{}{}
			} else {
				log.Printf("Received unknown message from client %d instead of ping", h.Seq)
			}
		}
	}()
	for ctx.Err() == nil {
		timeout, _ := context.WithTimeout(ctx, time.Minute)
		select {
		case <-timeout.Done():
			log.Printf("Watchdog triggered stop")
			cancel()
			cancel2()
		case <-ping:
		}
	}
}

func SendTunDev(sockPath string, tunfd *os.File) error {
	attempts := 0
	var err error
	for attempts < 1000 {
		var cnx0 net.Conn
		cnx0, err = net.Dial("unix", sockPath)
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			attempts++
			continue
		}
		defer cnx0.Close()
		cnx := cnx0.(*net.UnixConn)

		h := simpleipc.NewHeader(0, 0, []*os.File{tunfd})
		err = h.Write(cnx)
		if err != nil {
			return err
		}
		return nil
	}
	return err
}
