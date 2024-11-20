package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"syscall"

	flag "github.com/spf13/pflag"
	"golang.org/x/sys/unix"
	"kubevirt.io/client-go/log"
)

const (
	cgroupSysPath = "/sys/fs/cgroup"
	cgroupProcs   = "cgroup.procs"
	vfsdBin       = "/usr/libexec/virtiofsd"
	timeout       = 5
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

func getPid(socket string) (int, error) {
	sock, err := net.DialTimeout("unix", socket, time.Duration(timeout)*time.Second)
	if err != nil {
		return -1, err
	}
	defer sock.Close()

	ufile, err := sock.(*net.UnixConn).File()
	if err != nil {
		return -1, err
	}
	defer ufile.Close()

	// This is the tricky part, which will give us the PID of the owning socket
	ucreds, err := syscall.GetsockoptUcred(int(ufile.Fd()), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	if err != nil {
		return -1, err
	}

	if int(ucreds.Pid) == 0 {
		return -1, fmt.Errorf("the detected PID is 0. Is the isolation detector running in the host PID namespace?")
	}

	return int(ucreds.Pid), nil
}

type appDisaptcher struct {
	socket     string
	sharedDir  string
	contSocket string
}

func main() {
	var app appDisaptcher

	flag.StringVar(&app.socket, "socket-path", "", "Path for the virtiofs socket")
	flag.StringVar(&app.sharedDir, "shared-dir", "", "Shared directory for virtiofs")
	flag.StringVar(&app.contSocket, "cont-socket", "", "Path for the container socket")

	flag.Parse()

	if app.socket == "" {
		panic("socket path is empty")
	}
	if app.sharedDir == "" {
		panic("shared directory is empty")
	}
	if app.contSocket == "" {
		panic("the path for the container socket empty")
	}

	pid, err := getPid(app.contSocket)
	if err != nil {
		panic(err)
	}

	if err := moveIntoCgroup(pid); err != nil {
		panic(err)
	}
	if err := moveIntoProcNamespaces(pid); err != nil {
		panic(err)
	}

	cmd := exec.Command(vfsdBin,
		"--socket-path", app.socket,
		"--shared-dir", app.sharedDir,
		"--cache", "auto",
		"--sandbox", "none",
		"--log-level", "debug",
		"--xattr")

	// Redirect command output to the stdout and stderr of the container
	var fout, ferr io.Writer
	fout, err = os.OpenFile("/proc/1/fd/1", os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		panic(err)
	}
	ferr, err = os.OpenFile("/proc/1/fd/2", os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		panic(err)
	}
	cmd.Stderr = fout
	cmd.Stdout = ferr

	if err := cmd.Start(); err != nil {
		panic(err)
	}
}
