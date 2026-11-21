#!/bin/sh
# Re-apply the AI Console unlock patch after a DSM / package upgrade.
#
# Synology package upgrades overwrite libsynoai.so, which removes the patch.
# This wrapper runs the unlocker idempotently and records the outcome.
#
# Install:
#   sudo cp aiconsole-unlock /usr/local/bin/aiconsole-unlock
#   sudo chmod 755 /usr/local/bin/aiconsole-unlock
#   sudo cp post-upgrade.sh /usr/local/bin/aiconsole-unlock-reapply
#   sudo chmod 755 /usr/local/bin/aiconsole-unlock-reapply
#
# Then either run it by hand after every upgrade, or wire it into a boot task:
#   Control Panel > Task Scheduler > Create > Triggered Task
#   > Event: Boot-up > User: root
#   > Command: /usr/local/bin/aiconsole-unlock-reapply
#
# A boot task is the simplest reliable hook: DSM runs it after package services
# are already up, so the library is in place by then.

UNLOCK="${UNLOCK:-/usr/local/bin/aiconsole-unlock}"
LOG="${LOG:-/var/log/aiconsole-unlock.log}"

if [ ! -x "$UNLOCK" ]; then
    echo "error: $UNLOCK not found or not executable" >&2
    echo "       copy the aiconsole-unlock binary there first" >&2
    exit 1
fi

if [ "$(id -u)" -ne 0 ]; then
    echo "error: run as root (sudo $0)" >&2
    exit 1
fi

log() {
    printf '=== %s ===\n' "$(date -Iseconds)"
    printf '%s\n' "$@"
}

if "$UNLOCK" -status | grep -q 'already patched'; then
    log "state: already patched, nothing to do" | tee -a "$LOG"
    exit 0
fi

if "$UNLOCK" -apply; then
    log "result: patched" | tee -a "$LOG"
    exit 0
fi

rc=$?
log "result: FAILED (exit $rc)" \
    "        the AIConsole version probably changed its code layout." \
    "        see docs/internals.md for how to re-derive the signature." | tee -a "$LOG"
exit "$rc"
