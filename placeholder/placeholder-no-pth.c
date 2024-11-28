#include <stdio.h>
#include <unistd.h>
#include <stdarg.h>
#include <time.h>
#include <libgen.h>
#include <string.h>
#include <errno.h>
#include <sys/inotify.h>
#include <sys/epoll.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <linux/limits.h>
#include <stdbool.h>
#include <sys/syscall.h>

void error_log(const char *format, ...)
{
    va_list arglist;

    time_t ltime; /* calendar time */
    ltime=time(NULL); /* get current cal time */
    fprintf(stderr, "%s",asctime(localtime(&ltime)));
    fprintf(stderr, "error: ");
    va_start(arglist, format);
    vfprintf(stderr, format, arglist);
    va_end(arglist);
}

int pidfile_watcher(char *pidfile_dir)
{
    int fd = inotify_init1(0);
    if (fd < 0 ) goto err;

    int ret = inotify_add_watch(fd, pidfile_dir, IN_CREATE);
    if (ret < 0) goto err_close;

    return fd;
err_close:
    close(fd);
err:
    error_log("pidfile_watcher failed: %s", strerror(errno));
    return -1;
}

int read_pidfile (char *pidfile)
{
    int pid;

    FILE *f = fopen(pidfile,"r");
    if (!f) goto err;

    if (fscanf(f,"%d", &pid) < 0) goto err_close;

    fclose(f);

    return pid;
err_close:
    fclose(f);
err:
    error_log("read_pidfile failed: %s", strerror(errno));
    return -1;
}

int get_pidfd(int pid)
{
    int pidfd = syscall(SYS_pidfd_open, pid, 0);
    if (pidfd < 0) goto err;

    return pidfd;
err:
    error_log("get_pidfd failed: %s", strerror(errno));
    return -1;
}

bool pidfile_created(int fd, char *pidfile)
{
    size_t buf_size = sizeof(struct inotify_event) + PATH_MAX + 1;
    struct inotify_event event_buf[buf_size] = {};

    int len = read(fd, event_buf, buf_size);
    if (len < 0) goto err;

    struct inotify_event *event = (struct inotify_event *)event_buf;

    // FIXME: use strncmp with he correct size
    int ret = strcmp(pidfile, event->name);

    return ret == 0;

err:
    error_log("pidfile_created failed: %s", strerror(errno));
    return false;
}

int create_socket(const char *path)
{
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

// It takes the ownership of pidfile_watcher_fd and socket_fd
int monitor(char *pidfile_full_name, char *pidfile_name, int pidfile_watcher_fd, int socket_fd)
{
    int efd = epoll_create1(0);
    if (efd < 0 ) goto err;

    // Watch the pidfile_name directory
    struct epoll_event pidfile_event = {.events = EPOLLIN, .data.fd = pidfile_watcher_fd};
    if (epoll_ctl(efd, EPOLL_CTL_ADD, pidfile_watcher_fd, &pidfile_event) < 0) goto err;

    // Watch the socket
    // Even if we expect just one connection, we cannot use EPOLLONESHOT, because the dispatcher
    // could have died after connect() but before spawning virtiofsd, so we need to allow successive
    // connections.
    // FIXME: We should check if we receive a new connection while already monitoring a living pid
    struct epoll_event socket_event = {.events = EPOLLIN, .data.fd = socket_fd};
    if (epoll_ctl(efd, EPOLL_CTL_ADD, socket_fd, &socket_event) < 0) goto err;

    struct epoll_event epoll_events;
    while (true) {
        int ret = epoll_wait(efd, &epoll_events, 1, -1);
        if (ret < 0 && errno == EINTR) continue;
        if (ret < 0) goto err;

        if (epoll_events.data.fd == pidfile_watcher_fd) {
            if (!pidfile_created(epoll_events.data.fd, pidfile_name)) continue;

            // FIXME: Reading the pidfile is fundamentally racy,
            // the file may have been created but the pid has not been written yet
            int pid = read_pidfile(pidfile_full_name);
            if (pid < 0) goto err;

            int pidfd = get_pidfd(pid);
            if (pidfd < 0) goto err;

            struct epoll_event pidfd_event = {.events = EPOLLIN, .data.fd = pidfd};
            if (epoll_ctl(efd, EPOLL_CTL_ADD, pidfd, &pidfd_event) < 0) goto err;

        } else  if (epoll_events.data.fd == socket_fd) {
            int accept_fd = accept(socket_fd, NULL, NULL);
            if (accept_fd < 0) goto err;

            // Get a notification if the socket is closed, to avoid leaking the FD
            struct epoll_event acceptfd_event = {.events = EPOLLRDHUP | EPOLLONESHOT, .data.fd = accept_fd};
            // Ignore the error, If epoll_ctl fails we will just leak the accept_fd
            epoll_ctl(efd, EPOLL_CTL_ADD, accept_fd, &acceptfd_event);
        } else if (epoll_events.events & EPOLLRDHUP){
            // An event from the accepted connection, the other side closed the connection
            close(epoll_events.data.fd);
        } else {
            // pidfd event: the monitored process died
            close(epoll_events.data.fd);
            break;
        }
    }

    close(pidfile_watcher_fd);
    close(socket_fd);

    return 0;
err:
    error_log("monitor failed: %s", strerror(errno));

    close(pidfile_watcher_fd);
    close(socket_fd);
    return -1;
}

int main(int argc, char **argv) {
    // Both dirname and basename change their parameter
    char pidfile_path[PATH_MAX] = {};
    char pidfile_filename[PATH_MAX] = {};
    strcpy(pidfile_path, argv[1]);
    strcpy(pidfile_filename, argv[1]);

    char *pf_dir = dirname(pidfile_path);
    char *pf_name = basename(pidfile_filename);

    int pidfile_watcher_fd = pidfile_watcher(pf_dir);
    int socket_fd = create_socket("socket.sock");

    monitor(argv[1], pf_name, pidfile_watcher_fd, socket_fd);

    return 0;
}
