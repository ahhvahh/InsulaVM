#!/usr/bin/env bash
set -euo pipefail
test "$(uname -s)" = Linux
test -r /etc/os-release
. /etc/os-release
test "$ID" = debian
echo INSULA_TEST_OK
