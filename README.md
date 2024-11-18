# Dispatcher to launch virtiofsd in a container

## Build binaries and container
```bash
$  make all
```

## Run test container
```bash
$ podman run -v test:/test -td --name test virtiofs-placeholder
```

## Launch virtiofs
```bash
$ pid=$(podman inspect test   -f '{{ .State.Pid }}')
$ sudo ./vfsd-dispatcher --pid $pid --socket-path /var/run/vsfd.sock --shared-dir /test
```
