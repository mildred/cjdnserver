
#define _GNU_SOURCE

#include <fcntl.h>
#include <sched.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>
#include <errno.h>
#include <arpa/inet.h>

#include <linux/if.h>
#include <linux/in6.h>
#include <linux/ipv6.h>
#include <linux/if_tun.h>

static int socket_for_interface(const char *ifname, struct ifreq* ifRequestOut) {
    int s;

    s = socket(AF_INET6, SOCK_DGRAM, 0);
    if (s < 0) {
        return -errno;
    }

    memset(ifRequestOut, 0, sizeof(struct ifreq));
    strncpy(ifRequestOut->ifr_name, ifname, IFNAMSIZ);

    if (ioctl(s, SIOCGIFINDEX, ifRequestOut) < 0) {
        return -errno;
    }
    return s;
}

static int interface_up(const char *ifname) {
    struct ifreq ifreq = { 0 };
    int s = socket_for_interface(ifname, &ifreq);
    int ifIndex = ifreq.ifr_ifindex;
    if (s < 0) {
        perror("socket_for_interface");
        return -errno;
    }

    if (ioctl(s, SIOCGIFFLAGS, ifreq) < 0) {
        close(s);
        return -errno;
    }

    if (ifreq.ifr_flags & IFF_UP & IFF_RUNNING) {
        // already up.
        close(s);
        return 0;
    }

    ifreq.ifr_flags |= IFF_UP | IFF_RUNNING;
    if (ioctl(s, SIOCSIFFLAGS, ifreq) < 0) {
        close(s);
        return -errno;
    }
    close(s);
    return 0;
}

static int interface_add_addr(const char *ifname, struct in6_addr *ip, int prefixlen) {
    struct ifreq ifr;
    struct in6_ifreq ifr6;

    int s = socket(PF_INET6, SOCK_DGRAM, 0);
    if(s < 0) {
        perror("socket");
        return -errno;
    }

    memset (&ifr, 0, sizeof (struct ifreq));
    strncpy (ifr.ifr_name, ifname, IFNAMSIZ);
    if (-1 == ioctl(s, SIOGIFINDEX, &ifr)) {
        perror("ioctl(SIOGIFINDEX)");
        close(s);
        return -errno;
    }

    memset (&ifr6, 0, sizeof (struct in6_ifreq));
    ifr6.ifr6_addr = *ip;
    ifr6.ifr6_ifindex = ifr.ifr_ifindex;
    ifr6.ifr6_prefixlen = prefixlen;

    if (-1 == ioctl (s, SIOCSIFADDR, &ifr6)) {
        perror("ioctl(SIOCSIFADDR)");
        close(s);
        return -errno;
    }
    close(s);
    return 0;
#if 0

    struct in6_ifreq ifr6 = { 0 };
    ifr6.ifr6_ifindex = ifindex;
    ifr6.ifr6_prefixlen = prefixlen;
    memcpy(&ifr6.ifr6_addr, ip, sizeof(struct in6_addr));

    if (ioctl(s, SIOCSIFADDR, &ifr6) < 0) {
        return -errno;
    }
    return 0;
#endif
}

static int interface_set_mtu(const char *ifname, int mtu) {
    struct ifreq ifr;
    struct in6_ifreq ifr6;

    int s = socket(PF_INET6, SOCK_DGRAM, 0);
    if(s < 0) {
        perror("socket");
        return -errno;
    }

    memset (&ifr, 0, sizeof (struct ifreq));
    strncpy (ifr.ifr_name, ifname, IFNAMSIZ);
    ifr.ifr_mtu = mtu;

    if (-1 == ioctl(s, SIOCSIFMTU, &ifr)) {
        perror("ioctl(SIOCSIFMTU)");
        close(s);
        return -errno;
    }
    close(s);
    return 0;
#if 0
    struct ifreq req = { 0 };
    memcpy(&req, &ifreq, sizeof(struct ifreq));

    req.ifr_mtu = mtu;
    if (ioctl(s, SIOCSIFMTU, &req) < 0) {
        return -errno;
    }

    return 0;
#endif
}

static int configure_device(const char *ifname, struct in6_addr *ip, int prefixlen, int mtu) {
    if(interface_up(ifname) < 0) {
        perror("interface_up");
        return -errno;
    }

    if(interface_add_addr(ifname, ip, prefixlen) < 0) {
        perror("interface_add_addr");
        return -errno;
    }

    if(interface_set_mtu(ifname, mtu) < 0) {
        perror("interface_set_mtu");
        return -errno;
    }

    return 0;
}

static int create_tun_dev(int netnsfd, struct in6_addr *ip, int mtu) {
    int tunfd;
    struct ifreq ifr = { 0 };
    const char *desiredName = "cjdns0";
    const int prefixlen = 8;

    if (netnsfd >= 0) {
        if (setns(netnsfd, CLONE_NEWNET) == -1) {
            return -errno;
        }
    }

    memset(&ifr, 0, sizeof(ifr));
    ifr.ifr_flags = IFF_TUN;
    strncpy(ifr.ifr_name, desiredName, strlen(desiredName));
    tunfd = open("/dev/net/tun", O_RDWR);
    if (ioctl(tunfd, TUNSETIFF, (void *)&ifr) < 0) {
        return -errno;
    }

    if(configure_device(ifr.ifr_name, ip, prefixlen, mtu) < 0) {
        perror("configure_device");
        close(tunfd);
        return -errno;
    }

    return tunfd;
}

static int sendfd(int socket, int fd) {
    struct msghdr msg = { 0 };
    char buf[CMSG_SPACE(sizeof(fd))];
    memset(buf, '\0', sizeof(buf));
    struct iovec io;
    io.iov_base = 0;
    io.iov_len  = 0;

    msg.msg_iov = &io;
    msg.msg_iovlen = 1;
    msg.msg_control = buf;
    msg.msg_controllen = sizeof(buf);

    struct cmsghdr * cmsg = CMSG_FIRSTHDR(&msg);
    cmsg->cmsg_level = SOL_SOCKET;
    cmsg->cmsg_type = SCM_RIGHTS;
    cmsg->cmsg_len = CMSG_LEN(sizeof(fd));

    *((int *) CMSG_DATA(cmsg)) = fd;

    msg.msg_controllen = cmsg->cmsg_len;

    return sendmsg(socket, &msg, 0);
}


static int recvfd(int socket)  {
    struct msghdr msg = {0};

    char m_buffer[256];
    struct iovec io;
    io.iov_base = m_buffer;
    io.iov_len = sizeof(m_buffer);
    msg.msg_iov = &io;
    msg.msg_iovlen = 1;

    char c_buffer[256];
    msg.msg_control = c_buffer;
    msg.msg_controllen = sizeof(c_buffer);

    if(recvmsg(socket, &msg, 0) < 0) return -errno;

    struct cmsghdr * cmsg = CMSG_FIRSTHDR(&msg);
    unsigned char * data = CMSG_DATA(cmsg);

    int fd = *((int*) data);
    return fd;
}

int make_tun_dev(int netnsfd, const char *ipv6, int mtu) {
    int s[2];
    int tunfd = -EINVAL;
    struct in6_addr ipdata;

    int pton_res = inet_pton(AF_INET6, ipv6, &ipdata);
    if (pton_res < 0) {
        return -errno;
    } else if (pton_res == 0) {
        errno = EINVAL;
        return -EINVAL;
    }

    if(socketpair(AF_UNIX, SOCK_DGRAM, 0, s) < 0) {
        return -errno;
    }
    pid_t child = fork();
    if(child < 0) {
        return -errno;
    } else if(child == 0) {
        close(s[0]); // close read
        printf("[generate tun device] fork to netns fd=%d\n", netnsfd);
        tunfd = create_tun_dev(netnsfd, &ipdata, mtu);
        if(tunfd < 0) {
            perror("create_tun_dev");
            exit(1);
        }
        printf("[generate tun device] tun device created fd=%d\n", tunfd);
        if(sendfd(s[1], tunfd) < 0) {
            perror("sendfd");
            exit(1);
        }
        printf("[generate tun device] tun device sent to parent.\n");
        close(s[1]); // close write
        exit(0);
    } else {
        close(s[1]); // close write
        tunfd = recvfd(s[0]);
        if(tunfd < 0) {
            return -errno;
        }
        printf("[generate tun device] received tun device fd=%d\n", tunfd);
        close(s[0]); // close read
    }
    return tunfd;
}
