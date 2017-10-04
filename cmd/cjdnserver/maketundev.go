package main

/*
int make_tun_dev(int netns);
*/
import "C"

import (
	"os"
)

func MakeTunInNs(netns *os.File) (*os.File, error) {
	var netnsfd C.int = -1
	if netns != nil {
		netnsfd = C.int(netns.Fd())
	}
	tundevfd, err := C.make_tun_dev(netnsfd)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(tundevfd), "cjdns0"), nil
}
