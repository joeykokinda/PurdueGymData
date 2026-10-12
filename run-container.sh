#!/bin/sh
# Runs the site on brach in a locked-down container: it sees only its database directory,
# has no Linux capabilities, a read-only filesystem, and is reachable only from localhost (the tunnel).
set -eu
cd "$HOME/corec-tracker"
chmod 0755 corec-tracker-linux
docker build -q -t corec-tracker . >/dev/null
docker rm -f corec-tracker >/dev/null 2>&1 || true
docker run -d --name corec-tracker --restart unless-stopped \
  --user "$(id -u):$(id -g)" --read-only --tmpfs /tmp:size=64m \
  --cap-drop ALL --security-opt no-new-privileges \
  --memory 512m --cpus 1 --pids-limit 64 \
  -v "$HOME/corec-tracker/data:/data" -e DB_PATH=/data/corec.db -e ADDR=:8080 \
  -p 127.0.0.1:8610:8080 corec-tracker
