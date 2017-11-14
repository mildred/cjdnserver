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

The client side is not required if strict ordering is not necessary. The server
can lookup for containers starting up and add a cjdns interface to it
automatically.


Usage
=====

Client-side
-----------

Run `cjdnsclient` with a given socket file (probably something like
`/run/cjdnserver/cjdnserver.sock`) in the docker container. When the program
returns, you should have a configured interface.

You might want to specify the following options:

- the socket path
- the cjdns private key

Server-side
-----------

Run `cjdnserver`. You may want to configure using command line flags:

- the socket path
- the socket permissions (it cannot reuse an existing socket for now)
- the path to cjdroute if not in $PATH
- the UDP address, publickey and password of an upstream peer to connect to
  (detected from the running cjdns instance using the admin interface if not
  provided)

It is possible to select an automatic mode for the server side
(`-detect-netns`). In that case the client is not required to obtain a cjdns
address. All processes that do not share their parent process PID namespace and
that have a separate network namespace as the server are given a cjdns
interface.

Hacking
=======

TODO
----

- automatically add UDP peers on master cjdroute
- use admin interface to stop cjdroute instead of SIGTERM which takes longer

Client operation
----------------

- Open `/proc/self/ns/net`
- pass the namespace to the server
- wait for the server to tell us the tun interface is configured
- send watchdog in background

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
- Watch that cjdns keeps running and restart it if necessary
- Wait for the connection to close then kill cjdns
