FROM ubuntu:18.04@sha256:152dc042452c496007f07ca9127571cb9c29697f42acbfad72324b2bb2e43c98

ARG DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        bc \
        bison \
        build-essential \
        ca-certificates \
        cpio \
        device-tree-compiler \
        file \
        flex \
        git \
        kmod \
        libelf-dev \
        libncurses-dev \
        libssl-dev \
        libtinfo5 \
        python \
        python3 \
        rsync \
        xz-utils \
    && rm -rf /var/lib/apt/lists/*

ENV LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    OP7P_PROJECT_ROOT=/project \
    OP7P_WORK_ROOT=/work \
    OP7P_ARTIFACTS=/artifacts

WORKDIR /project
CMD ["bash"]
