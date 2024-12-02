#define _GNU_SOURCE
#include <getopt.h>
#include <stdio.h>
#include <stdarg.h>
#include <string.h>
#include <stdlib.h>
#include <limits.h>
#include <unistd.h>
#include <time.h>
#include <errno.h>
#include <sched.h>
#include <sys/syscall.h>

struct arguments {
        char socket_flag[PATH_MAX];
        char shareddir_flag[PATH_MAX];
	int pid;
};

struct arguments args;

static struct option long_options[] = {
    {"socket-path", required_argument, 0, 's'},
    {"shared-dir", required_argument, 0, 'd'},
    {"pid", required_argument, 0, 'p'},
    {0, 0, 0, 0}
};

static void usage() {
        printf("Placeholder for virtiofs\n"
               "Usage:\n"
               "\t-p, --pid:\t\tPid of the container\n"
	       "\t-d  --shared-dir\tShared directory flag for virtiofs\n"
	       "\t-s  --socket-path\tSocket path flag for virtiofs\n"
	       );
        exit(EXIT_FAILURE);
}

void parse_arguments(int argc, char **argv, struct arguments *args) {
    int c;
    while(1) {
        int option_index = 0;
        c = getopt_long(argc, argv, "d:p:s:", long_options, &option_index);

        if (c == -1) {
            break;
        }

        switch (c) {
            case 'd':
                strncpy(args->shareddir_flag, optarg, strlen(optarg));
                break;
            case 's':
                strncpy(args->socket_flag, optarg, strlen(optarg));
                break;
	    case 'p':
		args-> pid = atoi(optarg);
		break;
            case '?':
            default:
                usage();
                break;
        }
    }
}

void error_log(const char *format, ...)
{
    va_list arglist;

    time_t ltime;
    ltime=time(NULL);
    fprintf(stderr, "%s",asctime(localtime(&ltime)));
    fprintf(stderr, "error: ");
    va_start(arglist, format);
    vfprintf(stderr, format, arglist);
    va_end(arglist);
}

int move_into_cgroup(pid_t pid)
{
	char path[PATH_MAX - 30];
	char syspath[PATH_MAX];
	FILE *fptr;
	char str[20];

	snprintf(path, PATH_MAX, "/proc/%d/cgroup", pid);
	fptr = fopen(path, "r");
	if (fptr == NULL) goto err;
	fgets(path, sizeof(path), fptr);
	fclose(fptr);


	snprintf(path, strlen(path) - 4, path + 4);

	snprintf(syspath, PATH_MAX, "/sys/fs/cgroup/%s/cgroup.procs", path);
	fprintf(stderr, "move the process into the cgroup as %s\n", path);
	fptr = fopen(syspath, "a");
	if (fptr == NULL ) goto err;
	sprintf(str, "%d", getpid());
	fputs(str, fptr);
	fclose(fptr);

	return 0;
err:
	error_log("failed to move process into cgroup path %s: %s", syspath, strerror(errno));
	return -1;
}

int move_into_namespaces(pid_t pid)
{
	fprintf(stderr, "move the process into same namespaces as %d\n", pid);
        int fd = syscall(SYS_pidfd_open, pid, 0);
	if (fd < 0) goto err;
        if (setns(fd, CLONE_NEWNET|
		  CLONE_NEWPID|
		  CLONE_NEWIPC|
		  CLONE_NEWNS|
		  CLONE_NEWCGROUP|
		  CLONE_NEWUTS) < 0) goto err;

        return 0;
err:
	error_log("failed to move process into the namespace: %s", strerror(errno));
	return -1;
}

int main(int argc, char **argv)
{
	parse_arguments(argc, argv, &args);

	if(move_into_cgroup(args.pid) < 0 )
		exit(EXIT_FAILURE);
	if(move_into_namespaces(args.pid) < 0 )
		exit(EXIT_FAILURE);

	// Fork is to create a process in the pid namesace
	pid_t child =  fork();
	if (child < 0)
		exit(EXIT_FAILURE);
	if (child > 0)
		exit(EXIT_SUCCESS);

	char bin[] = "/usr/libexec/virtiofsd";
	char *virtiofs_argv[] = {
		bin,
		"--socket-path", args.socket_flag,
		"--shared-dir", args.shareddir_flag,
		"--cache", "auto",
		"--sandbox", "none",
		"--log-level", "debug",
		"--xattr", NULL };
	char *env[] = { NULL };

	if (freopen("/proc/1/fd/1", "a", stdout) == NULL) {
		error_log("failed redirecting stdout: %s", strerror(errno));
		exit(EXIT_FAILURE);
	}
	if (freopen("/proc/1/fd/2", "a", stderr) == NULL){
		error_log("failed redirecting stdout: %s", strerror(errno));
		exit(EXIT_FAILURE);
	}

	if (daemon(0, -1) != 0) {
		error_log("failed daemon: %s", strerror(errno));
		exit(EXIT_FAILURE);
	}
	fprintf(stderr, "start virtiofs\n");

	if (execve(bin, virtiofs_argv, env) < 0) {
		error_log("failed executing virtiofs: %s", strerror(errno));
		exit(EXIT_FAILURE);
	}
}
