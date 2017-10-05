package main

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"os/exec"
)

func Genconf(cjdroute, tunsockpath, adminaddr string, peer *Peer) (string, string, error) {
	cmd := exec.Command("sh", "-xc", cjdroute+" --genconf | "+cjdroute+" --cleanconf")

	out, err := cmd.Output()
	if err != nil {
		log.Print(string(err.(*exec.ExitError).Stderr))
		return "", "", err
	}

	var config map[string]interface{} = map[string]interface{}{}
	err = json.Unmarshal(out, &config)
	if err != nil {
		return "", "", err
	}

	interfaces_udp_0 := config["interfaces"].(map[string]interface{})["UDPInterface"].([]interface{})[0].(map[string]interface{})
	interfaces_udp_0_connectTo := interfaces_udp_0["connectTo"].(map[string]interface{})
	if peer.Address != "" {
		interfaces_udp_0_connectTo[peer.Address] = map[string]interface{}{
			"password":  peer.Password,
			"publicKey": peer.Pubkey,
		}
	}
	interfaces_eth_0 := config["interfaces"].(map[string]interface{})["ETHInterface"].([]interface{})[0].(map[string]interface{})
	interfaces_eth_0["bind"] = "lo"
	logging := config["logging"].(map[string]interface{})
	logging["logTo"] = "stdout"
	admin := config["admin"].(map[string]interface{})
	admin["bind"] = adminaddr
	router_interface := config["router"].(map[string]interface{})["interface"].(map[string]interface{})
	router_interface["tunfd"] = "normal"
	router_interface["tunDevice"] = tunsockpath
	ipv6 := config["ipv6"].(string)

	data, err := json.MarshalIndent(config, "", " ")
	return string(data), ipv6, err
}

func Start(cjdroute, config string) error {
	cmd := exec.Command(cjdroute, "--nobg")
	cmd.Stdin = bytes.NewReader([]byte(config))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
