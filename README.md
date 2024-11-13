# Dispatcher to launch virtiofsd in a container

## Build test container
```bash
$  podman build -t test .
```

## Run test container
```bash
$ podman run -v test:/test -td --name test test
```

## Launch virtiofs
```bash
$ pid=$(podman inspect test   -f '{{ .State.Pid }}')
$ sudo ./vfsd-dispatcher --pid $pid --socket-path /var/run/vsfd.sock --shared-dir /test
```
