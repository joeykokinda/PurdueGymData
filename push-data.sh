#!/bin/sh
# Nightly on brach via crontab: export every reading to data/YYYY-MM.csv and push if anything changed.
set -eu
cd "$HOME/PurdueGymData"
git pull -q --rebase
DB_PATH="$HOME/corec-tracker/data/corec.db" "$HOME/corec-tracker/corec-tracker" export data
git add data
git diff --cached --quiet || { git commit -qm "Data through $(date -u +%F)"; git push -q; }
