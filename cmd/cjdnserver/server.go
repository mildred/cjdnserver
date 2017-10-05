package main

import (
	"flag"
	"fmt"
	"github.com/jbenet/go-reuseport"
	"github.com/mildred/simpleipc"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path"
	"strconv"
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
	flag.StringVar(&sockPath, "sock", "cjdserver.sock", "Socket file path")
	flag.StringVar(&perms, "perms", "0755", "Socket permissions")
	flag.StringVar(&cjdroute, "cjdroute", "cjdroute", "cjdroute executable")
	flag.StringVar(&peer.Address, "peer-address", "", "Peer address to connect to over UDP")
	flag.StringVar(&peer.Password, "peer-password", "", "Peer password")
	flag.StringVar(&peer.Pubkey, "peer-pubkey", "", "Peer public key")
	flag.Parse()

	perms1, _ := strconv.ParseInt(perms, 8, 32)

	err := run(cjdroute, sockPath, os.FileMode(perms1), &peer)
	if err != nil {
		log.Fatal(err)
	}
}

func run(cjdroute, sockPath string, perms os.FileMode, peer *Peer) error {
	var l net.Listener
	f, err := os.OpenFile(sockPath, 0, 0)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Remove %s", sockPath)
			err = os.Remove(sockPath)
		}
		log.Printf("Listen on %s", sockPath)
		l, err = net.Listen("unix", sockPath)
	} else {
		l, err = net.FileListener(f)
		f.Close()
	}
	if err != nil {
		return err
	}
	defer l.Close()

	log.Printf("Chmod %s to 0%s", sockPath, strconv.FormatInt(int64(perms), 8))
	err = os.Chmod(sockPath, perms)
	if err != nil {
		log.Print(err)
	}

	for {
		cnx, err := l.Accept()
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}
		go (func() {
			err := handleClient(cnx.(*net.UnixConn), cjdroute, peer)
			if err != nil {
				log.Print(err)
			}
		})()
	}

	return nil
}

func handleClient(cnx *net.UnixConn, cjdroute string, peer *Peer) error {
	var h simpleipc.Header

	tmpdir, err := ioutil.TempDir("", "cjdnserver-client")
	if err != nil {
		return err
	}
	log.Printf("Receive client in %v", tmpdir)
	defer os.RemoveAll(tmpdir)

	adminif, err := reuseport.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	adminaddr := adminif.LocalAddr().String()
	log.Printf("Listen to admin %v", adminaddr)
	defer adminif.Close()

	sockpath := path.Join(tmpdir, "cjdnstun.socket")
	conf, ipv6, err := Genconf(cjdroute, sockpath, adminaddr, peer)
	if err != nil {
		return err
	}

	conffile := path.Join(tmpdir, "cjdroute.conf")
	err = ioutil.WriteFile(conffile, []byte(conf), 0644)
	if err != nil {
		return err
	}

	log.Printf("Configuration file written to %s", conffile)
	log.Print(conf)

	err = h.Read(cnx, nil)
	if err != nil {
		return err
	}
	log.Printf("Received header %#v", h)
	if len(h.Files) == 0 {
		return fmt.Errorf("Did not received any file descriptor")
	}
	tunfd, err := MakeTunInNs(h.Files[0], ipv6, InterfaceMTU)
	_ = tunfd
	if err != nil {
		return err
	}

	go (func() {
		err := SendTunDev(sockpath, tunfd)
		if err != nil {
			log.Print(err)
		}
	})()

	log.Printf("Start cjdroute")
	err = Start(cjdroute, conf)
	if err != nil {
		return err
	}

	return nil
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
