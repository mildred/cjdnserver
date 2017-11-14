package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
)

func GetPPidOf(pid int) (int, error) {
	data, err := ioutil.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return -1, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		cols := strings.SplitN(line, ":", 2)
		if cols[0] == "PPid" {
			return strconv.Atoi(strings.TrimSpace(cols[1]))
		}
	}
	return -1, fmt.Errorf("No PPid in /proc/%d/status", pid)
}

func GetEnvironOf(pid int, name string) (string, error) {
	data, err := ioutil.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return "", err
	}
	for _, env := range bytes.Split(data, []byte{0}) {
		cols := strings.SplitN(string(env), "=", 2)
		if cols[0] == name {
			return cols[1], nil
		}
	}
	return "", nil
}
