package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/fc00/go-cjdns/admin"
	"github.com/fc00/go-cjdns/key"
	"github.com/jbenet/go-reuseport"
	"github.com/mildred/cjdnserver"
	"github.com/mildred/cjdnserver/genpass"
	"github.com/mildred/simpleipc"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/user"
	"path"
	"regexp"
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
	var detectNetNs bool
	flag.StringVar(&sockPath, "sock", "/run/cjdnserver/cjdserver.sock", "Socket file path")
	flag.StringVar(&perms, "perms", "0755", "Socket permissions")
	flag.StringVar(&cjdroute, "cjdroute", "cjdroute", "cjdroute executable")
	flag.StringVar(&peer.Address, "peer-address", "0.0.0.0:33097", "Peer address to connect to over UDP")
	flag.StringVar(&peer.Password, "peer-password", "", "Peer password")
	flag.StringVar(&peer.Pubkey, "peer-pubkey", "", "Peer public key")
	flag.BoolVar(&detectNetNs, "detect-netns", false, "Detect network namespace and instanciate cjdns for them")
	flag.Parse()

	perms1, _ := strconv.ParseInt(perms, 8, 32)

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	cjdnserver.CancelSignals(ctx, &wg, cancel, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	err := run(ctx, &wg, cjdroute, sockPath, os.FileMode(perms1), &peer, detectNetNs)
	if err != nil {
		log.Fatal(err)
	}
}

func run(ctx0 context.Context, wg *sync.WaitGroup, cjdroute, sockPath string, perms os.FileMode, peer *Peer, detectNetNs bool) error {
	ctx, cancel := context.WithCancel(ctx0)

	var adm *admin.Conn
	if peer.Pubkey == "" || peer.Password == "" {
		var err error
		var config *admin.CjdnsAdminConfig = &admin.CjdnsAdminConfig{
			Addr:     "127.0.0.1",
			Port:     11234,
			Password: "NONE",
		}

		u, err := user.Current()
		if err != nil {
			return err
		}

		rawFile, err := ioutil.ReadFile(u.HomeDir + "/.cjdnsadmin")
		if err == nil {
			raw, err := stripComments(rawFile)
			if err != nil {
				return err
			}

			err = json.Unmarshal(raw, &config)
			if err != nil {
				return err
			}
		}

		adm, err = admin.Connect(config)
		if err != nil {
			return err
		}
	}
	if peer.Pubkey == "" {
		node, err := adm.NodeStore_nodeForAddr("")
		if err != nil {
			return err
		}
		peer.Pubkey = node.Key
	}
	if peer.Password == "" && peer.Pubkey == "" {
		peer.Password = genpass.Generate(32)
		err := adm.AuthorizedPasswords_add("cjdnserver peers", peer.Password, 0)
		if err != nil {
			return err
		}

		defer func() {
			err := adm.AuthorizedPasswords_remove("cjdnserver peers")
			if err != nil {
				log.Println(err)
			}
		}()
	}

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

	if detectNetNs {
		wg.Add(1)
		go func() {
			err := detectProcesses(ctx, wg, cjdroute, peer)
			if err != nil {
				log.Print(err)
			}
			cancel()
		}()
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
			err := handleClient(ctx, wg, &SimpleIPCClientCnx{cnx.(*net.UnixConn)}, cjdroute, peer)
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

type SimpleIPCClientCnx struct {
	cnx *net.UnixConn
}

func (c *SimpleIPCClientCnx) ReceivePrivKey() (*key.Private, *os.File, error) {
	h := new(simpleipc.Header)
	privkey, err := h.ReadWithPayload(c.cnx, nil)
	if err != nil {
		return nil, nil, err
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
		return nil, nil, fmt.Errorf("Did not received any file descriptor")
	}
	return skey, h.Files[0], nil
}

func (c *SimpleIPCClientCnx) SendInitialResponse() error {
	h := simpleipc.NewHeader(cjdnserver.InitialResponse, 0, nil)
	return h.Write(c.cnx)
}

func (c *SimpleIPCClientCnx) ReceivePing(ctx context.Context) (error, bool) {
	h := new(simpleipc.Header)
	_, err := h.ReadWithPayload(c.cnx, nil)
	if err != nil {
		return fmt.Errorf("Received error: %s", err), true
	} else if h.Seq == cjdnserver.WatchdogPing {
		return nil, false
	} else {
		return fmt.Errorf("Received unknown message from client %d instead of ping", h.Seq), false
	}
}

type ClientCnx interface {
	// Return a secret key (or nil, it is optional), a file corresponding to the
	// network namespace file descriptor and an error
	ReceivePrivKey() (*key.Private, *os.File, error)

	// Unlock the client side when the cjdns interface is ready
	SendInitialResponse() error

	// Wait and return when the client sends a watchdog ping. Return an error and
	// a boolean indicating if the error is fatal or not.
	ReceivePing(ctx context.Context) (error, bool)
}

type DetectedNamespace struct {
	Ino      uint64
	SKey     *key.Private
	File     *os.File
	Cancel   context.CancelFunc
	Watchdog chan struct{}
	Mark     bool
}

func (ns *DetectedNamespace) ReceivePrivKey() (*key.Private, *os.File, error) {
	return nil, ns.File, nil
}

func (ns *DetectedNamespace) SendInitialResponse() error {
	return nil
}

func (ns *DetectedNamespace) ReceivePing(ctx context.Context) (error, bool) {
	select {
	case <-ctx.Done():
		return nil, false
	case <-ns.Watchdog:
		return nil, false
	}
}

func mark(list map[uint64]*DetectedNamespace) {
	for _, ns := range list {
		ns.Mark = true
	}
}

func sweep(list map[uint64]*DetectedNamespace) {
	for ino, ns := range list {
		if ns.Mark {
			ns.Cancel()
			delete(list, ino)
		}
	}
}

func detectProcesses(ctx context.Context, wg *sync.WaitGroup, cjdroute string, peer *Peer) error {
	nsList := map[uint64]*DetectedNamespace{}

	for ctx.Err() == nil {
		mark(nsList)
		//log.Printf("Detect new network namespaces...")

		selfNsSt, err := os.Stat("/proc/self/ns/net")
		if err != nil {
			return err
		}
		selfNsInode := selfNsSt.Sys().(*syscall.Stat_t).Ino

		proc, err := os.Open("/proc")
		if err != nil {
			return err
		}
		defer proc.Close()

		pids, err := proc.Readdirnames(-1)
		if err != nil {
			return err
		}
		for _, pidName := range pids {
			//log.Printf("Detect new network namespaces... pid %s", pidName)
			pid, err := strconv.Atoi(pidName)
			if err != nil {
				continue
			}
			netnsName := fmt.Sprintf("/proc/%s/ns/net", pidName)
			se, err := os.Stat(netnsName)
			if err != nil {
				//log.Printf("%s: %v", netnsName, err)
				continue
			}
			inode := se.Sys().(*syscall.Stat_t).Ino
			if inode == selfNsInode {
				continue
			}
			ppid, err := GetPPidOf(pid)
			if err != nil {
				log.Printf("/proc/%s/status: %v", pidName, err)
				continue
			}
			if ppid == 0 {
				continue // the process is our init system
			}
			pidnsName := fmt.Sprintf("/proc/%d/ns/pid", pid)
			ppidnsName := fmt.Sprintf("/proc/%d/ns/pid", ppid)
			pidnsSt, err := os.Stat(pidnsName)
			if err != nil {
				log.Printf("%s: %v", pidnsName, err)
				continue
			}
			ppidnsSt, err := os.Stat(ppidnsName)
			if err != nil {
				log.Printf("%s: %v", ppidnsName, err)
				continue
			}
			if pidnsSt.Sys().(*syscall.Stat_t).Ino == ppidnsSt.Sys().(*syscall.Stat_t).Ino {
				continue // the process is not the init process of a container
			}
			nsFile, err := os.Open(netnsName)
			if err != nil {
				log.Printf("%s: %v", netnsName, err)
				continue
			}
			skeystr, err := GetEnvironOf(pid, "CJDNS_PRIVKEY")
			if err != nil {
				log.Printf("/proc/%d/environ: %v", pid, err)
				continue
			}
			var skey *key.Private
			if skeystr != "" {
				skey, err = key.DecodePrivate(skeystr)
				if err != nil {
					log.Printf("parse secret key: %v", netnsName, err)
					continue
				}
			}
			if ns, ok := nsList[inode]; ok {
				ns.Mark = false
				ns.Watchdog <- struct{}{}
			} else {
				log.Printf("New network namespace for pid %s: %d", pidName, inode)
				nsCtx, nsCancel := context.WithCancel(ctx)
				ns := &DetectedNamespace{
					Ino:      inode,
					SKey:     skey,
					File:     nsFile,
					Cancel:   nsCancel,
					Watchdog: make(chan struct{}, 0),
				}
				nsList[inode] = ns
				wg.Add(1)
				go (func() {
					defer wg.Done()
					err := handleClient(nsCtx, wg, ns, cjdroute, peer)
					if err != nil {
						log.Print(err)
					}
				})()
			}
		}

		sweep(nsList)

		tmout, _ := context.WithTimeout(ctx, time.Second)
		<-tmout.Done()
	}
	return nil
}

func handleClient(ctx0 context.Context, wg *sync.WaitGroup, cnx ClientCnx, cjdroute string, peer *Peer) error {
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

	skey, tunfile, err := cnx.ReceivePrivKey()
	if err != nil {
		return err
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

	tunfd, err := MakeTunInNs(tunfile, ipv6, InterfaceMTU)
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
		instanceCtx, instanceStop := context.WithCancel(ctx)
		log.Printf("Start cjdroute")
		process, err := Start(cjdroute, conf)
		if err != nil {
			return err
		}

		go (func() {
			cnxtun, err := SendTunDev(sockpath, tunfd)
			if err != nil {
				log.Print(err)
			}
			defer cnxtun.Close()
			<-instanceCtx.Done()
		})()

		err = cnx.SendInitialResponse()
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
		instanceStop()
	}

	log.Printf("Stopped %s", ipv6)

	return nil
}

func receiveWatchdog(ctx0 context.Context, wg *sync.WaitGroup, cancel context.CancelFunc, cnx ClientCnx) {
	ctx, cancel2 := context.WithCancel(ctx0)
	ping := make(chan struct{})
	//wg.Add(1)
	go func() {
		//defer wg.Done()
		for ctx.Err() == nil {
			err, fatal := cnx.ReceivePing(ctx)
			if err != nil {
				log.Print(err)
				if fatal {
					log.Printf("Watchdog triggered stop")
					cancel()
					cancel2()
				}
			} else {
				ping <- struct{}{}
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

func SendTunDev(sockPath string, tunfd *os.File) (net.Conn, error) {
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
		cnx := cnx0.(*net.UnixConn)

		h := simpleipc.NewHeader(0, 0, []*os.File{tunfd})
		err = h.Write(cnx)
		if err != nil {
			return nil, err
		}
		return cnx0, nil
	}
	return nil, err
}

func stripComments(b []byte) ([]byte, error) {
	regComment, err := regexp.Compile("(?s)//.*?\n|/\\*.*?\\*/")
	if err != nil {
		return nil, err
	}
	out := regComment.ReplaceAllLiteral(b, nil)
	return out, nil
}
