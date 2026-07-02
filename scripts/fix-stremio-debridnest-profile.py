#!/usr/bin/env python3
"""Remove stale DebridNest addon entries from Stremio 5 LocalStorage profile."""

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
    raw = conn.execute("SELECT value FROM ItemTable WHERE key='profile'").fetchone()[0]
    if isinstance(raw, str):
        raw = raw.encode('latin1')
    return json.loads(raw.decode('utf-16')), raw


def save_profile(conn, profile):
    encoded = json.dumps(profile, separators=(',', ':')).encode('utf-16')
    conn.execute("UPDATE ItemTable SET value=? WHERE key='profile'", (encoded,))
    conn.commit()


def main():
    if not PROFILE_DB.exists():
        print(f'Profile not found: {PROFILE_DB}', file=sys.stderr)
        return 1

    conn = sqlite3.connect(PROFILE_DB)
    profile, _ = load_profile(conn)
    before = len(profile.get('addons', []))
    profile['addons'] = [
        addon for addon in profile.get('addons', [])
        if 'debridnest' not in addon.get('manifest', {}).get('id', '').lower()
        and 'debridnest' not in addon.get('manifest', {}).get('name', '').lower()
    ]
    removed = before - len(profile['addons'])
    save_profile(conn, profile)
    print(f'Removed {removed} DebridNest addon entr{"y" if removed == 1 else "ies"} from Stremio profile.')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
