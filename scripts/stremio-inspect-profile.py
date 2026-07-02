#!/usr/bin/env python3
"""Print installed Stremio addons and transport URLs from local profile."""

import json
import sqlite3
import sys
from pathlib import Path

PROFILE_DB = Path.home() / (
    'Library/WebKit/com.westbridge.stremio5-mac/WebsiteData/Default/'
    'OJXUE1G37eNPeSzOef6Qe22HDEEWMOHX82pveunDErQ/'
    'OJXUE1G37eNPeSzOef6Qe22HDEEWMOHX82pveunDErQ/LocalStorage/localstorage.sqlite3'
)


def load_profile(conn):
    row = conn.execute("SELECT value FROM ItemTable WHERE key='profile'").fetchone()
    if row is None:
        raise SystemExit('No profile key in LocalStorage')
    raw = row[0]
    if isinstance(raw, str):
        raw = raw.encode('latin1')
    return json.loads(raw.decode('utf-16'))


def main():
    if not PROFILE_DB.exists():
        print(f'Profile not found: {PROFILE_DB}', file=sys.stderr)
        return 1

    profile = load_profile(sqlite3.connect(PROFILE_DB))
    addons = profile.get('addons', [])
    print(f'Installed addons: {len(addons)}\n')
    for i, addon in enumerate(addons, 1):
        manifest = addon.get('manifest', {})
        transport = addon.get('transportUrl') or addon.get('transport_url') or ''
        print(f'{i}. {manifest.get("name", "?")} v{manifest.get("version", "?")}')
        print(f'   id: {manifest.get("id", "?")}')
        print(f'   transport: {transport[:120]}{"..." if len(transport) > 120 else ""}')
        flags = manifest.get('behaviorHints') or {}
        if flags:
            print(f'   behaviorHints: {flags}')
        print()
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
