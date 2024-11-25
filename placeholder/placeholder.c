#include <getopt.h>
#include <string.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <stdbool.h>
#include <unistd.h>
#include <time.h>
#include <pthread.h>
#include <errno.h>
#include <limits.h>
#include <sys/inotify.h>
#include <sys/epoll.h>
#include <sys/syscall.h>
#include <unistd.h>
#include <sys/socket.h>
#include <sys/un.h>

struct arguments {
	char pidfile[PATH_MAX];
	char socket[PATH_MAX];
};

static struct option long_options[] = {
    {"socket-path", required_argument, 0, 'c'},
    {"pidfile", required_argument, 0, 'p'},
    {0, 0, 0, 0}
};

static void usage() {
	printf("Placeholder for virtiofs\n"
	       "Usage:\n"
	       "\t-p, --pidfile:\tPid of process to monitor\n"
	       "\t-s, --socket:\tContainer socket path to retrieve the pid\n");
	exit(EXIT_FAILURE);
}

void parse_arguments(int argc, char **argv, struct arguments *args) {
    int c;
    while(1) {
        /* getopt_long stores the option index here. */
        int option_index = 0;
        c = getopt_long(argc, argv, "c:p:", long_options, &option_index);

        /* Detect the end of the options. */
        if (c == -1) {
            break;
        }

        switch (c) {
            case 'c':
                strncpy(args->socket, optarg, strlen(optarg));
                break;
            case 'p':
		strncpy(args->pidfile, optarg, strlen(optarg));
		break;
            case '?':
            default:
                usage();
		break;
        }
    }
}

void dir(char * path, char *dst){
	char t[PATH_MAX];
        char *last;
	char *p = t;

	strncpy(t, path, strlen(path));
        p = strtok(p, "/");
        while(p != NULL){
                last = p;
                p = strtok(NULL, "/");
        }
	strncpy(dst, path, strlen(path) - strlen(last) - 1);
}

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

void *start_monitor_proc_virtiofs(void *args) {
	char *pidfile = (char *)args;
	char d[PATH_MAX];
        struct epoll_event epoll_events;
        struct epoll_event event;
	int efd, fd;

	dir(pidfile, d);

        if ((fd = inotify_init1(0)) < 0 ) {
                error_log("inotify_init1 failed: %s",  strerror(errno));
		pthread_exit(NULL);
	}
	if (inotify_add_watch(fd, d, IN_CREATE|IN_OPEN|IN_ACCESS|IN_MODIFY) < 0) {
                error_log("inotify_add_watch failed: %s", strerror(errno));
		pthread_exit(NULL);
        }
	if ((efd = epoll_create1(0)) < 0 ) {
                error_log("epoll_create1 failed: %s", strerror(errno));
        }
	event.events = EPOLLIN;
	event.data.fd = fd;

        if (epoll_ctl(efd, EPOLL_CTL_ADD, fd, &event) != 0) {
                error_log("epoll_ctl for monitoring the pidfile failed: %s", strerror(errno));
		pthread_exit(NULL);
	}

        while(true) {
		if(epoll_wait(efd, &epoll_events, 1, -1) < 0) {
			error_log("epoll_wait for monitoring the pidfile failed: %s", strerror(errno));
			pthread_exit(NULL);
		}
		int ret = access(pidfile, F_OK);
		if (ret < 0 && errno != ENOENT) {
			error_log("failed to access the pidfile %s: %s", pidfile, strerror(errno));
			pthread_exit(NULL);
		}
		if (ret == 0) {
			char t[100];
			FILE *f = fopen(pidfile, "r");
			fgets(t, 100, f);
			pid_t pid = (int) strtol(t, NULL, 10);
			pthread_exit(&pid);
		}
        }
}

int create_socket(char *path) {
	int fd = socket(AF_UNIX, SOCK_STREAM, 0);
	struct sockaddr_un addr;

	if (fd < 0) {
		error_log("socket failed: %s", strerror(errno));
		return -1;
	}

	addr.sun_family = AF_UNIX;
        strncpy(addr.sun_path, path, sizeof(addr.sun_path) - 1);
	if (bind(fd, (struct sockaddr *) &addr, sizeof(addr)) == -1) {
		error_log("bind failed: %s", strerror(errno));
		return -1;
	}
	printf("listening at: %s\n", addr.sun_path);
	if (listen(fd, 1) == -1) {
		error_log("listen failed: %s", strerror(errno));
		return -1;
	}
	if (accept(fd, NULL, NULL) < 0) {
		error_log("accept failed: %s", strerror(errno));
		return -1;
	}

	return 0;
}

int main(int argc, char **argv) {
	struct arguments args;
	pthread_t thread_id;
	void* resp;
	struct epoll_event epoll_events;
	struct epoll_event event;
	int efd, fd;
	pid_t pid;

	parse_arguments(argc, argv, &args);

	printf("start monitoring for virtiofs\n");

	if (create_socket(args.socket) < 0)
		exit(EXIT_FAILURE);

	pthread_create(&thread_id, NULL, start_monitor_proc_virtiofs, &args.pidfile);
	pthread_join(thread_id, &resp);

	pid = *(int *)resp;
	printf("virtiofs started with pid %d\n", pid);
	if (pid < 0) {
		exit(EXIT_FAILURE);
	}

	if ((fd = syscall(SYS_pidfd_open, pid, 0)) < 0) {
		error_log("pidfd_open failed: %s", strerror(errno));
		exit(EXIT_FAILURE);
	}

	if ((efd = epoll_create1(0)) < 0 ) {
                error_log("epoll_create1 for monitoring the pid process failed: %s", strerror(errno));
		exit(EXIT_FAILURE);
        }
	event.events = EPOLLIN;
	event.data.fd = fd;

        if (epoll_ctl(efd, EPOLL_CTL_ADD, fd, &event) != 0) {
                error_log("epoll_ctl for monitoring the pid process failed: %s", strerror(errno));
	}

	if(epoll_wait(efd, &epoll_events, 1, -1) < 0) {
		error_log("epoll_wait for monitoring the pid process failed: %s", strerror(errno));
		exit(EXIT_FAILURE);
	}

	printf("virtiofs process terminated\n");
}
