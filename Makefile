IMAGE=virtiofs-placeholder

all: binaries image

binaries:
	go build -o vfsd-dispatcher main.go
	go build -o vfsd-placeholder placeholder/placeholder.go

image:
	podman build -t $(IMAGE) -f Dockerfile .
