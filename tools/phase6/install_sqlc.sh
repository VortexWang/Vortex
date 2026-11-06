#!/bin/sh
set -eu
destination=${1:?destination required}
case "$(uname -s)-$(uname -m)" in
  Linux-aarch64|Linux-arm64) platform=linux_arm64; checksum=b7cae247740d0c51a1e657479e5b2d21e6fef428f596682a01bc55bf4ab8a23d ;;
  Linux-x86_64) platform=linux_amd64; checksum=497ae4fcdfa64c5b0c311ffe4c2bd991e43991e82e5367792ed78bc2dca27354 ;;
  Darwin-arm64) platform=darwin_arm64; checksum=21602158c99eb1f2bae197a66abfb1941d1e9e50b23125bb193349c6b1acc71e ;;
  *) echo 'unsupported sqlc platform' >&2; exit 1 ;;
esac
mkdir -p "$destination"
curl --fail --location --silent --show-error "https://github.com/sqlc-dev/sqlc/releases/download/v1.31.1/sqlc_1.31.1_${platform}.tar.gz" -o "$destination/sqlc.tar.gz"
actual=$(shasum -a 256 "$destination/sqlc.tar.gz" | cut -d ' ' -f 1)
test "$actual" = "$checksum"
tar -xzf "$destination/sqlc.tar.gz" -C "$destination" sqlc
test "$("$destination/sqlc" version)" = v1.31.1
