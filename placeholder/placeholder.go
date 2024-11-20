package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"kubevirt.io/client-go/log"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"
)

const (
	virtiofsBin = "virtiofsd"
)

func findVirtiofsPidInProc() (int, error) {
	files, err := os.ReadDir("/proc")
	if err != nil {
		return -1, err
	}

	for _, file := range files {
		name := file.Name()
		n, err := strconv.Atoi(name)
		// Not a number
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", name, "cmdline"))
		if err != nil {
			continue
		}
		if strings.Contains(string(data), virtiofsBin) {
			return n, nil
		}
	}

	return -1, fmt.Errorf("Not found")
}

func startMonitorProcForVirtiofs(c chan int) error {
	fd, err := unix.InotifyInit()
	if err != nil {
		fmt.Errorf("inotify_init failed: %w", err)
	}
	_, err = unix.InotifyAddWatch(fd, "/proc", unix.IN_CREATE|unix.IN_OPEN|unix.IN_ACCESS|unix.IN_MODIFY)
	if err != nil {
		return fmt.Errorf("inotify_add_watch failed: %w", err)
	}
	efd, err := unix.EpollCreate1(0)
	if err != nil {
		return fmt.Errorf("epoll_create1 for proc monitoring failed: %w", err.Error())
	}
	e := unix.EpollEvent{
		Events: unix.EPOLLIN,
		Fd:     int32(fd),
	}
	if err := unix.EpollCtl(efd, unix.EPOLL_CTL_ADD, fd, &e); err != nil {
		return fmt.Errorf("epoll_ctl for proc monitoring failed: %w", err)
	}
	var pid int
	epollEvents := []unix.EpollEvent{{}}
	for {
		_, err = unix.EpollWait(efd, epollEvents, -1)
		if err != nil {
			return fmt.Errorf("epoll_wait for proc monitoring failed: %w", err)
		}
		pid, err = findVirtiofsPidInProc()
		if err == nil {
			break
		}
	}
	c <- pid

	return nil
}

func watchProcForVirtiofs(socket string) error {
	logger := log.DefaultLogger()
	c := make(chan int, 1)
	var g errgroup.Group

	g.Go(func() error {
		return startMonitorProcForVirtiofs(c)
	})
	go startMonitorProcForVirtiofs(c)
	logger.Infof("Start monitoring proc")

	// Let the dispatcher connect to signal it got the pid of the current process
	l, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	conn, err := l.Accept()
	if err != nil {
		return err
	}
	conn.Close()
	logger.Info("Dispatcher connected")

	if err := g.Wait(); err != nil {
		return err
	}
	pid := <-c

	logger.Infof("Started virtiofs with pid: %d", pid)

	efd, err := unix.EpollCreate1(0)
	if err != nil {
		return fmt.Errorf("epoll_create1 for for virtiofs monitoring failed: %w", err.Error())
	}
	// Monitor the virtiofs pid file descriptor.
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return err
	}
	e := unix.EpollEvent{
		Events: unix.EPOLLIN,
		Fd:     int32(fd),
	}
	epollEvents := []unix.EpollEvent{{}}
	if err := unix.EpollCtl(efd, unix.EPOLL_CTL_ADD, fd, &e); err != nil {
		return fmt.Errorf("epoll_ctl for virtiofs monitoring failed: %w", err)
	}
	_, err = unix.EpollWait(efd, epollEvents, -1)
	if err != nil {
		return fmt.Errorf("epoll_wait for virtiofs monitoring failed: %w", err)
	}

	logger.Infof("Virtiofs process terminated")

	return nil
}

func main() {
	var socket string

	flag.StringVar(&socket, "socket-path", "", "Path for the socket")
	flag.Parse()

	if socket == "" {
		panic("The --socket-path needs to be set")
	}

	if _, err := os.Stat(socket); err == nil {
		os.Remove(socket)
	}

	err := watchProcForVirtiofs(socket)
	if err != nil {
		panic(err)
	}
}
