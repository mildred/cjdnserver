package main

import (
	"flag"
	"github.com/mildred/cjdnserver"
	"github.com/mildred/simpleipc"
	"log"
	"net"
	"os"
)

func main() {
	var sockPath string
	flag.StringVar(&sockPath, "sock", "cjdserver.sock", "Socker file path")
	flag.Parse()

	err := run(sockPath)
	if err != nil {
		log.Fatal(err)
	}
}

func run(sockPath string) error {
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
	err = h.Write(cnx)
	if err != nil {
		return err
	}

	return nil
}
