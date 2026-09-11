#!/bin/sh
set -eu

snow_examples_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd -P)
exec "${SNOW_BIN:-snow}" \
  --js-plugin "$snow_examples_dir/project-context" \
  --js-plugin "$snow_examples_dir/todo-radar" \
  --js-plugin "$snow_examples_dir/git-review" \
  "$@"
