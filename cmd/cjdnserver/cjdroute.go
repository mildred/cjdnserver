package main

import (
	"bytes"
	"encoding/json"
	"github.com/fc00/go-cjdns/key"
	"log"
	"os"
	"os/exec"
)

func Genconf(cjdroute, tunsockpath, adminaddr string, peer *Peer, skey *key.Private) (string, string, string, error) {
	cmd := exec.Command("sh", "-xc", cjdroute+" --genconf --no-eth | "+cjdroute+" --cleanconf")

	out, err := cmd.Output()
	if err != nil {
		log.Print(string(err.(*exec.ExitError).Stderr))
		return "", "", "", err
	}

	var config map[string]interface{} = map[string]interface{}{}
	err = json.Unmarshal(out, &config)
	if err != nil {
		return "", "", "", err
	}

	if skey != nil {
		config["privateKey"] = skey.String()
		config["publicKey"] = skey.Pubkey().String()
		config["ipv6"] = skey.Pubkey().IP().String()
	}

	interfaces_udp_0 := config["interfaces"].(map[string]interface{})["UDPInterface"].([]interface{})[0].(map[string]interface{})
	interfaces_udp_0_connectTo := interfaces_udp_0["connectTo"].(map[string]interface{})
	if peer.Address != "" {
		interfaces_udp_0_connectTo[peer.Address] = map[string]interface{}{
			"password":  peer.Password,
			"publicKey": peer.Pubkey,
		}
	}
	//logging := config["logging"].(map[string]interface{})
	//logging["logTo"] = "stdout"
	admin := config["admin"].(map[string]interface{})
	admin["bind"] = adminaddr
	adminpass := admin["password"].(string)
	router_interface := config["router"].(map[string]interface{})["interface"].(map[string]interface{})
	router_interface["tunfd"] = "normal"
	router_interface["tunDevice"] = tunsockpath
	ipv6 := config["ipv6"].(string)

	data, err := json.MarshalIndent(config, "", " ")
	return string(data), ipv6, adminpass, err
}

func Start(cjdroute, config string) (*os.Process, error) {
	cmd := exec.Command(cjdroute, "--nobg")
	cmd.Stdin = bytes.NewReader([]byte(config))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Start()
	return cmd.Process, err
}
