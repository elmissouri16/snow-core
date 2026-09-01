#!/bin/sh
set -eu

contents=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
if [ -z "${SNOW_BINARY:-}" ]; then
  SNOW_BINARY="$contents/Resources/snow"
  export SNOW_BINARY
fi
if [ -z "${SNOW_PROJECT:-}" ]; then
  SNOW_PROJECT=${HOME:?HOME is required when SNOW_PROJECT is unset}
  export SNOW_PROJECT
fi
exec "$contents/MacOS/snow-desktop-bin" "$@"
