# Isolated build environment

## Boundary

- Builder: Docker image `oneplus7pro-kernel-builder:ubuntu18.04-v1`
- Base image: `ubuntu:18.04@sha256:152dc042452c496007f07ca9127571cb9c29697f42acbfad72324b2bb2e43c98`
- Project mount: `/project`, read-only
- Working source/build mount: `/work`, Docker volume
  `oneplus7pro-kernel-work`
- Export mount: `/artifacts`, mapped only to `06-artifacts/`

The read-only project boundary and writable work volume were tested from a
throwaway container. Compiler and package dependencies are contained in the
image and are not installed by the build scripts into the normal Ubuntu WSL
distribution.

## Verified tools

```text
Android clang version 6.0.2, build 4691093
MagiskBoot - Boot Image Modification Tool
Python 2 for the original OnePlus kernel wrappers; Python 3 is also available
```

The OnePlus Makefile hard-codes `scripts/gcc-wrapper.py`, which aborts on every
compiler warning. The build script overrides `CC` on the make command line so
normal warnings remain visible without being promoted by that private wrapper.
Kbuild's targeted `-Werror` checks for unsafe language errors remain enabled.

## Host-side note

Before container isolation was requested, a WSL preparation attempt created
`/home/<user>/op7p-kernel-work` and Ubuntu completed installation of build
packages before the outer command was stopped. That directory and those
packages are not used by this project. They have deliberately not been removed
automatically because uninstalling packages could affect unrelated WSL work.
