package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"syscall"

	flag "github.com/spf13/pflag"
	"kubevirt.io/client-go/log"
)

const (
	cgroupSysPath = "/sys/fs/cgroup"
	cgroupProcs   = "cgroup.procs"
	binary        = "/usr/libexec/virtiofsd"
)

func sys_setns() uintptr {
	switch runtime.GOARCH {
	case "amd64":
		return 308
	default:
		panic("arch not recognized")
	}
}

func sys_pidfd_open() uintptr {
	switch runtime.GOARCH {
	case "amd64":
		return 434
	default:
		panic("arch not recognized")
	}
}

func setns(fd int, nstype uintptr) error {
	_, _, err := syscall.Syscall6(sys_setns(), uintptr(fd), nstype, 0, 0, 0, 0)
	if err != 0 {
		return fmt.Errorf("setns failed with errno: %s", err.Error())
	}

	return nil
}

func pidfd_open(pid int, flags uint) (int, error) {
	fd, _, err := syscall.Syscall6(sys_pidfd_open(), uintptr(pid), uintptr(flags), 0, 0, 0, 0)
	if err != 0 {
		return -1, fmt.Errorf("pidfd_open failed with errno: %s", err.Error())
	}

	return int(fd), nil
}

func moveIntoCgroup(pid int) error {
	content, err := ioutil.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return err
	}

	s := strings.TrimPrefix(string(content), "0::/")
	s = strings.Trim(s, "\n")
	path := filepath.Join(cgroupSysPath, s, cgroupProcs)
	log.DefaultLogger().Infof("Move the process into the cgroup %s", path)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err = f.WriteString(strconv.Itoa(syscall.Getpid())); err != nil {
		return err
	}

	return nil
}

func moveIntoProcNamespaces(pid int) error {
	log.DefaultLogger().Infof("Move the process into same namespaces as %d", pid)
	fd, err := pidfd_open(pid, 0)
	if err != nil {
		return err
	}
	if err := setns(fd, syscall.CLONE_NEWNET|
		syscall.CLONE_NEWPID|
		syscall.CLONE_NEWIPC|
		syscall.CLONE_NEWNS|
		syscall.CLONE_NEWCGROUP|
		syscall.CLONE_NEWUTS); err != nil {
		return err
	}

	return nil
}

type appDisaptcher struct {
	pid       int
	socket    string
	sharedDir string
}

func main() {
	var app appDisaptcher

	flag.IntVar(&app.pid, "pid", 0, "Pid of the container where to dispatch virtiofs")
	flag.StringVar(&app.socket, "socket-path", "", "Path for the virtiofs socket")
	flag.StringVar(&app.sharedDir, "shared-dir", "", "Shared directory for virtiofs")
	flag.Parse()

	if app.pid < 0 {
		panic("invalid pid")
	}
	if app.socket == "" {
		panic("socket path is empty")
	}
	if app.sharedDir == "" {
		panic("shared directory is empty")
	}

	if err := moveIntoCgroup(app.pid); err != nil {
		panic(err)
	}
	if err := moveIntoProcNamespaces(app.pid); err != nil {
		panic(err)
	}

	args := []string{"virtiofsd", "--socket-path", app.socket, "--shared-dir", app.sharedDir}
	if err := syscall.Exec(binary, args, os.Environ()); err != nil {
		panic(err)
	}
}
