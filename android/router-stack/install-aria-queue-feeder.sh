#!/data/data/com.termux/files/usr/bin/sh
set -eu

# This automatic queue feeder was retired. AriaNg is the only authority for
# aria2 concurrency and pause/resume choices.
HOME=/data/data/com.termux/files/home
PREFIX=/data/data/com.termux/files/usr
SERVICE="$HOME/router-stack/services/aria-queue"
if [ -f "$HOME/.stage/aria-queue-feeder.sh" ]; then
  install -m 700 "$HOME/.stage/aria-queue-feeder.sh" \
    "$HOME/router-stack/bin/aria-queue-feeder"
fi
mkdir -p "$SERVICE"
: > "$SERVICE/down"
"$PREFIX/bin/sv" down "$SERVICE" >/dev/null 2>&1 || true
echo 'aria-queue remains disabled; manage concurrency in AriaNg.'
