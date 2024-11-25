IMAGE=virtiofs-placeholder
PLACEHOLDER=vfsd-placeholder

all: binaries image

binaries: placeholder
	go build -o vfsd-dispatcher main.go

.PHONY: placeholder
placeholder:
	$(CC) -o $(PLACEHOLDER) -Wall placeholder/placeholder.c

image:
	podman build -t $(IMAGE) -f Dockerfile .
