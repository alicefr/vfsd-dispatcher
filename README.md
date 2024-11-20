# Dispatcher to launch virtiofsd in a container

## Build binaries and container
```bash
$  make all
```

## Run test container
```bash
$ mkdir -p /tmp/test
$ podman run -v test:/test -td --name test \
    --security-opt label:disable \
    -v /tmp/test:/var/run/vfsd:Z \
    virtiofs-placeholder --socket-path /var/run/vfsd/placeholder.sock
```

## Launch virtiofs
```bash
$ sudo ./vfsd-dispatcher --cont-socket /tmp/test/placeholder.sock --cont-socket /tmp/test/placeholder.sock \
    --socket-path /var/run/vsfd.sock --shared-dir /test
```
