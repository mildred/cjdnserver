Client operation
----------------

- Open `/proc/self/ns/net`
- pass the namespace to the server
- TODO: wait for the server to tell us the tun interface is configured
- TODO: call subprocess or exec to it, keep the connection open either in the main
  process or a subprocess

Server Operation
----------------

- Receive net namespace file descritor
- Invoke a new process with this file descriptor

    - call setns on the network namespace fd to switch network namespace
    - create tun interface by opening /dev/net/tun and ioctl TUNSETIFF
    - pass the tun file descriptor back to the server

- When receiving the tun file descriptor from the helper process, create a pipe
  to pass the tun file descriptor to cjdns
- Allocate a port for the admin api on loopback
- Create a cjdns configuration file with the necessary configuration to access
  the tun device
- Start cjdns
- TODO: set tun interface address and MTU within the net namespace
- TODO: Watch that cjdns keeps running and restart it if necessary
- TODO: Wait for the connection to close then kill cjdns
