#!/system/bin/sh
set -eu

PREFIX=/data/data/com.termux/files/usr
HOME=/data/data/com.termux/files/home
SERVICE="$HOME/router-stack/services/openlist"

"$PREFIX/bin/sv" status "$SERVICE" || true
"$PREFIX/bin/curl" -fsS http://127.0.0.1:5244/ping || true
echo
echo SERVICE_FILES
find "$SERVICE" -maxdepth 3 -type f -print
echo RECENT_LOG
if [ -f "$SERVICE/log/main/current" ]; then
    tail -n 100 "$SERVICE/log/main/current"
elif [ -f "$SERVICE/log/current" ]; then
    tail -n 100 "$SERVICE/log/current"
else
    find "$HOME/router-data" -maxdepth 4 -type f -name current -print
fi
