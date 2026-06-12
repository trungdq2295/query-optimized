#!/bin/sh
# Container entrypoint: for a SQLite deploy, load the demo data on first boot
# (the db lives on a volume, so this runs once). For MySQL, QOPT_ENGINE=mysql
# skips this and the data is seeded by the compose 'seed' service instead.
set -e

if [ "${QOPT_ENGINE:-sqlite}" = "sqlite" ] && [ ! -f "$QOPT_DSN" ]; then
  echo "▸ seeding demo data into $QOPT_DSN"
  mkdir -p "$(dirname "$QOPT_DSN")"
  /app/qopt-seed -engine sqlite -dsn "$QOPT_DSN" -file /app/examples/seed-sqlite.sql
fi

exec /app/qopt-server
