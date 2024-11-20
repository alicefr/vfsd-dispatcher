package main

import (
	"errors"
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

func startMonitorProcForVirtiofs(c chan int, pidfile string) error {
	dir := filepath.Dir(pidfile)
	fd, err := unix.InotifyInit()
	if err != nil {
		fmt.Errorf("inotify_init failed: %w", err)
	}
	_, err = unix.InotifyAddWatch(fd, dir, unix.IN_CREATE|unix.IN_OPEN|unix.IN_ACCESS|unix.IN_MODIFY)
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
	epollEvents := []unix.EpollEvent{{}}
	for {
		_, err = unix.EpollWait(efd, epollEvents, -1)
		if err != nil {
			return fmt.Errorf("epoll_wait for proc monitoring failed: %w", err)
		}
		_, err := os.Stat(pidfile)
		if errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		data, err := os.ReadFile(pidfile)
		if err != nil {
			return err
		}
		pid, err := strconv.Atoi(strings.Trim(string(data), "\n"))
		if err != nil {
			return err
		}
		c <- pid
		break
	}

	return nil
}

func watchProcForVirtiofs(socket, pidfile string) error {
	logger := log.DefaultLogger()
	c := make(chan int, 1)
	var g errgroup.Group

	g.Go(func() error {
		return startMonitorProcForVirtiofs(c, pidfile)
	})
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
	var pidfile string

	flag.StringVar(&socket, "socket-path", "", "Path for the socket")
	flag.StringVar(&pidfile, "pidfile", "", "Path for virtiofs")
	flag.Parse()

	if socket == "" {
		panic("The --socket-path needs to be set")
	}
	if pidfile == "" {
		panic("The --pidfile needs to be set")
	}

	if _, err := os.Stat(socket); err == nil {
		os.Remove(socket)
	}

	err := watchProcForVirtiofs(socket, pidfile)
	if err != nil {
		panic(err)
	}
}
