#!/bin/sh
# Bind the Go session package into an Android archive. The phone then uses the
# same Noise and ticket code as the desktop client.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
export ANDROID_HOME="${ANDROID_HOME:-/tmp/android-sdk}"
export ANDROID_NDK_HOME="${ANDROID_NDK_HOME:-$ANDROID_HOME/ndk/26.1.10909125}"
if [ -x /home/ubuntu/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.0.linux-amd64/bin/go ]; then
  PATH="/home/ubuntu/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.0.linux-amd64/bin:$PATH"
fi
export PATH="${PATH}:$(go env GOPATH)/bin"
export GOTOOLCHAIN=local
mkdir -p "$root/android/app/libs"
cd "$root/mobile"
gomobile bind -target=android -androidapi 28 -javapkg=cyandesk -o "$root/android/app/libs/cyandesk.aar" .
