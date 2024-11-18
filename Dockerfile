FROM quay.io/centos/centos:stream9

RUN dnf install -y strace virtiofsd && dnf remove --all
COPY ./vfsd-placeholder /usr/bin/vfsd-placeholder

ENTRYPOINT ["/usr/bin/vfsd-placeholder"]
