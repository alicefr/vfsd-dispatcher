FROM quay.io/centos/centos:stream9

RUN dnf install -y virtiofsd && dnf remove --all
