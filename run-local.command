#!/usr/bin/env bash
# run-local.command — macOS double-click launcher. Finder runs this in a new
# Terminal window; it just hands off to run-local.sh in the same folder.
cd "$(dirname "$0")"
exec ./run-local.sh
