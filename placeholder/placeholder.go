package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"kubevirt.io/client-go/log"

	"golang.org/x/sys/unix"
)

const virtiofsBin = "virtiofsd"

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

func watchProcForVirtiofs() error {
	log.DefaultLogger().Infof("Start monitoring proc")
	fd, err := unix.InotifyInit()
	if err != nil {
		return fmt.Errorf("inotify_init failed: %w", err)
	}
	_, err = unix.InotifyAddWatch(fd, "/proc", unix.IN_CREATE|unix.IN_OPEN)
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
		//if err != nil && !IsRestartError(err) {
		if err != nil {
			return fmt.Errorf("epoll_wait for proc monitoring failed: %w", err)
		}
		pid, err = findVirtiofsPidInProc()
		if err == nil {
			break
		}
	}

	log.DefaultLogger().Infof("Started virtiofs with pid: %d", pid)

	efd, err = unix.EpollCreate1(0)
	if err != nil {
		return fmt.Errorf("epoll_create1 for for virtiofs monitoring failed: %w", err.Error())
	}
	// Monitor the virtiofs pid file descriptor.
	fd, err = unix.PidfdOpen(pid, 0)
	if err != nil {
		return err
	}
	e = unix.EpollEvent{
		Events: unix.EPOLLIN,
		Fd:     int32(fd),
	}
	if err := unix.EpollCtl(efd, unix.EPOLL_CTL_ADD, fd, &e); err != nil {
		return fmt.Errorf("epoll_ctl for virtiofs monitoring failed: %w", err)
	}
	_, err = unix.EpollWait(efd, epollEvents, -1)
	if err != nil {
		return fmt.Errorf("epoll_wait for virtiofs monitoring failed: %w", err)
	}

	log.DefaultLogger().Infof("Virtiofs process terminated")

	return nil
}

func main() {
	err := watchProcForVirtiofs()
	if err != nil {
		panic(err)
	}
}
