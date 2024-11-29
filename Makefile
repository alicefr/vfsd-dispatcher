IMAGE=virtiofs-placeholder
PLACEHOLDER=vfsd-placeholder
DISPATCHER=vfsd-dispatcher

all: binaries image

binaries: placeholder dispatcher

.PHONY: placeholder
placeholder:
	$(CC) -o $(PLACEHOLDER) -Wall placeholder/placeholder.c

dispatcher:
	$(CC) -o $(DISPATCHER) -Wall main.c

image:
	podman build -t $(IMAGE) -f Dockerfile .
