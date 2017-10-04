
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

#include <linux/if.h>
#include <linux/if_tun.h>

static int create_tun_dev(int netnsfd) {
    int tunfd;
    struct ifreq ifr;

    if (netnsfd >= 0) {
        if (setns(netnsfd, CLONE_NEWNET) == -1) {
            return -errno;
        }
    }

    memset(&ifr, 0, sizeof(ifr));
    ifr.ifr_flags = IFF_TUN;
    tunfd = open("/dev/net/tun", O_RDWR);
    if (ioctl(tunfd, TUNSETIFF, (void *)&ifr) < 0) {
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

int make_tun_dev(int netnsfd) {
    int s[2];
    int tunfd = -EINVAL;

    if(socketpair(AF_UNIX, SOCK_DGRAM, 0, s) < 0) {
        return -errno;
    }
    pid_t child = fork();
    if(child < 0) {
        return -errno;
    } else if(child == 0) {
        close(s[0]); // close read
        printf("[generate tun device] fork to netns fd=%d\n", netnsfd);
        tunfd = create_tun_dev(netnsfd);
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
