# Restore drill log

Append-only. Each successful run of `scripts/backup/drill.sh` writes
one line. A missing entry over a quarterly window means the drill
was skipped — the audit should treat that as a finding.

## Entries

<!-- newest at the top, written by drill.sh -->
