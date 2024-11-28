#include <stdio.h>
#include <unistd.h>
#include <stdarg.h>
#include <time.h>
#include <string.h>
#include <stdbool.h>
#include <signal.h>
#include <errno.h>
#include <sys/epoll.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <sys/signalfd.h>

void error_log(const char *format, ...) {
    va_list arglist;

    time_t ltime; /* calendar time */
    ltime = time(NULL); /* get current cal time */
    fprintf(stderr, "%s", asctime(localtime(&ltime)));
    fprintf(stderr, "error: ");
    va_start(arglist, format);
    vfprintf(stderr, format, arglist);
    va_end(arglist);
}

int get_signalfd(int signal) {
    sigset_t sigset;
    sigemptyset(&sigset);
    sigaddset(&sigset, signal);
    int fd = signalfd(-1, &sigset, SFD_NONBLOCK);
    if (fd < 0) {
        error_log("create_socket failed: %s", strerror(errno));
    }
    return fd;
}

int create_socket(const char *path) {
    int fd = socket(AF_UNIX, SOCK_STREAM, 0);

    if (fd < 0) goto err;

    struct sockaddr_un addr = {};
    addr.sun_family = AF_UNIX;
    strncpy(addr.sun_path, path, sizeof(addr.sun_path) - 1);

    if (bind(fd, (struct sockaddr *) &addr, sizeof(addr)) < 0) goto err_close;

    if (listen(fd, 1) < 0) goto err_close;

    return fd;
err_close:
    close(fd);
err:
    error_log("create_socket failed: %s", strerror(errno));
    return -1;
}

int monitor(int socket_fd, int sig_fd) {
    int efd = epoll_create1(0);
    if (efd < 0) goto err;

    // Watch the socket
    // Even if we expect just one connection, we cannot use EPOLLONESHOT, because the dispatcher
    // could have died after connect() but before spawning virtiofsd, so we need to allow successive
    // connections.
    struct epoll_event socket_event = {.events = EPOLLIN, .data.fd = socket_fd};
    if (epoll_ctl(efd, EPOLL_CTL_ADD, socket_fd, &socket_event) < 0) goto err;

    struct epoll_event signal_event = {.events = EPOLLIN, .data.fd = sig_fd};
    if (epoll_ctl(efd, EPOLL_CTL_ADD, sig_fd, &signal_event) < 0) goto err;

    struct epoll_event epoll_events;
    while (true) {
        int ret = epoll_wait(efd, &epoll_events, 1, -1);
        if (ret < 0 && errno == EINTR) continue;
        if (ret < 0) goto err;

        if (epoll_events.data.fd == sig_fd) {
            printf("Signal!! \n");
            struct signalfd_siginfo sfdi;
            int len = read(epoll_events.data.fd, &sfdi, sizeof(sfdi));
            if (len == sizeof(sfdi)) break;
        } else if (epoll_events.data.fd == socket_fd) {
            int accept_fd = accept(socket_fd, NULL, NULL);
            if (accept_fd < 0) goto err;

            // Get a notification if the socket is closed, to avoid leaking the FD
            struct epoll_event acceptfd_event = {.events = EPOLLRDHUP | EPOLLONESHOT, .data.fd = accept_fd};
            // Ignore the error, If epoll_ctl fails we will just leak the accept_fd
            epoll_ctl(efd, EPOLL_CTL_ADD, accept_fd, &acceptfd_event);
        } else if (epoll_events.events & EPOLLRDHUP) {
            // An event from the accepted connection, the other side closed the connection
            close(epoll_events.data.fd);
        }
    }

    close(socket_fd);
    close(sig_fd);

    return 0;
err:
    error_log("monitor failed: %s", strerror(errno));

    close(socket_fd);
    close(sig_fd);

    return -1;
}

int main(int argc, char **argv) {

    sigset_t sigset;
    /* Block all signals */
    sigfillset(&sigset);
    sigprocmask(SIG_BLOCK, &sigset, NULL);

    int sig_fd = get_signalfd(SIGCHLD);
    int socket_fd = create_socket("socket.sock");

    return monitor(socket_fd, sig_fd);
}