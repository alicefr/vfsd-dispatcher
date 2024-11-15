package main

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"syscall"

	flag "github.com/spf13/pflag"
	"golang.org/x/sys/unix"
	"kubevirt.io/client-go/log"
)

const (
	cgroupSysPath = "/sys/fs/cgroup"
	cgroupProcs   = "cgroup.procs"
	binary        = "/usr/libexec/virtiofsd"
)

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
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return err
	}
	if err := unix.Setns(fd, unix.CLONE_NEWNET|
		unix.CLONE_NEWPID|
		unix.CLONE_NEWIPC|
		unix.CLONE_NEWNS|
		unix.CLONE_NEWCGROUP|
		unix.CLONE_NEWUTS); err != nil {
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

	cmd := exec.Command("/usr/libexec/virtiofsd",
		"--socket-path", app.socket,
		"--shared-dir", app.sharedDir,
		"--cache", "auto",
		"--sandbox", "none",
		"--xattr")
	if err := cmd.Start(); err != nil {
		panic(err)
	}
}
