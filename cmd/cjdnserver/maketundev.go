package main

/*
int make_tun_dev(int netns, const char *ipv6, int mtu);
*/
import "C"

import (
	"os"
)

func MakeTunInNs(netns *os.File, ipv6 string, mtu int) (*os.File, error) {
	var netnsfd C.int = -1
	if netns != nil {
		netnsfd = C.int(netns.Fd())
	}
	tundevfd, err := C.make_tun_dev(netnsfd, C.CString(ipv6), C.int(mtu))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(tundevfd), "cjdns0"), nil
}
