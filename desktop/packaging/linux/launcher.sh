#!/bin/sh
set -eu

bin_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
root=$(CDPATH= cd -- "$bin_dir/.." && pwd -P)
if [ -z "${SNOW_BINARY:-}" ]; then
  SNOW_BINARY="$root/libexec/snow-desktop/snow"
  export SNOW_BINARY
fi
if [ -z "${SNOW_PROJECT:-}" ]; then
  SNOW_PROJECT=${HOME:?HOME is required when SNOW_PROJECT is unset}
  export SNOW_PROJECT
fi
exec "$root/libexec/snow-desktop/snow-desktop-bin" "$@"
