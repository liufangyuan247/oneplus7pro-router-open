#!/data/data/com.termux/files/usr/bin/sh
PREFIX='/data/data/com.termux/files/usr'
STACK='/data/data/com.termux/files/home/router-stack'
SERVICES='/data/data/com.termux/files/home/router-stack/services'
PATH="$PREFIX/bin"

case "${1:-status}" in
  start)
    : > "$STACK/enabled"
    echo 'Router stack enabled; the Magisk keeper will start it within 30 seconds.'
    ;;
  stop)
    rm -f "$STACK/enabled"
    for service in "$SERVICES"/*; do
      "$PREFIX/bin/sv" down "$service" 2>/dev/null || true
    done
    ;;
  restart)
    for service in "$SERVICES"/*; do
      "$PREFIX/bin/sv" restart "$service" 2>/dev/null || true
    done
    ;;
  status)
    for service in "$SERVICES"/*; do
      "$PREFIX/bin/sv" status "$service" 2>/dev/null || true
    done
    echo '--- listeners ---'
    su -c "$PREFIX/bin/ss -lnt" 2>/dev/null |
      grep -E ':(5244|6800|6880|8022|8080|8317|9090)[[:space:]]' || true
    ;;
  logs)
    service="${2:-healthd}"
    tail -n 80 "$SERVICES/$service/log/main/current"
    ;;
  *)
    echo 'usage: routerctl {start|stop|restart|status|logs SERVICE}' >&2
    exit 2
    ;;
esac
