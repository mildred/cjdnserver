cjdnserver
==========

This is a client server that works over unix domain sockets. The server is
responsible of maintaining cjdroute instances up and running for a client
located in a restricted environment with limited access rights and possibly in a
network namespace.

The goal is to embed the client in docker containers (or any other container
technology) and share the socket with both the host and container. On the host,
the server is running and starting cjdns instances hooked to a tun interface in
the network namespace.

Unix domain sockets are required to pass around file descriptors such as the
network namespace file descriptor and tun device file descriptor.


Usage
=====

Client-side
-----------

Run `cjdnsclient` with a given socket file (probably something like
`/run/cjdnserver/cjdnserver.sock`) in the docker container

Server-side
-----------

Run `cjdnserver`. You may want to configure using command line flags:

- the socket path
- the socket permissions (it cannot reuse an existing socket for now)
- the path to cjdroute if not in $PATH
- the DUP address, publickey and password of an upstream peer to connect to

Hacking
=======

TODO
----

- restart cjdroute on crash
- detect container shutdown and stop cjdroute

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
- Allocate a port for the admin api on loopback
- Create a cjdns configuration file with the necessary configuration to access
  the tun device
- Invoke a new process with this file descriptor

    - call setns on the network namespace fd to switch network namespace
    - create tun interface by opening /dev/net/tun and ioctl TUNSETIFF
    - bring the interface up with the correct address
    - pass the tun file descriptor back to the server

- When receiving the tun file descriptor from the helper process, create a pipe
  to pass the tun file descriptor to cjdns
- Start cjdns
- TODO: Watch that cjdns keeps running and restart it if necessary
- TODO: Wait for the connection to close then kill cjdns
