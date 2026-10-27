# Guest source provenance

The active macOS backend uses native Apple Virtualization.framework, not Firecracker. `firecracker-lock.json`, its license and the optional fetch script are retained for the future Linux backend; Firecracker archives are not linked into the macOS app.

Active input hashes and immutable filenames are in `macos-guest-lock.json`:

- Alpine minirootfs 3.23.7, aarch64, from the official Alpine release mirror. Its packages retain their upstream licenses. Alpine build recipes: https://gitlab.alpinelinux.org/alpine/aports/-/tree/3.23-stable.
- Alpine `linux-virt` 6.18.59-r0, aarch64. Linux is GPL-2.0-only. Corresponding vanilla kernel source: https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-6.18.59.tar.xz. The Alpine build configuration and patches are in `main/linux-lts` at aports commit `8604a852cc2f621ad7e77032ab54f066d1ffaad2`: https://gitlab.alpinelinux.org/alpine/aports/-/tree/8604a852cc2f621ad7e77032ab54f066d1ffaad2/main/linux-lts. The complete package is downloaded and its included module dependencies are preserved; only its EFI zboot wrapper is decompressed for Apple’s raw ARM64 Linux boot loader.
- CPython 3.12.16, Astral python-build-standalone release 20261026, `aarch64-unknown-linux-musl-install_only_stripped`. Source and distribution licenses: https://github.com/astral-sh/python-build-standalone/tree/20261026 and https://github.com/python/cpython/tree/v3.12.16.

The guest agent and kernel control script are authored in this repository under `cmd/guest/`. `scripts/build-guest.py` is the complete image assembly recipe. It constructs cpio records directly and does not run Linux tools or third-party install scripts on the Mac.
