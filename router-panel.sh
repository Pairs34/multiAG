#!/usr/bin/env sh
set -eu
AG_PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec python3 "$AG_PROJECT_DIR/scripts/router_trial.py" panel --state-dir "$AG_PROJECT_DIR/.local/router-trial"
