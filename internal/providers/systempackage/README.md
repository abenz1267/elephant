### Elephant System Packages

Cross-distro package management: search, install, remove, and show package info.

#### Features

- Supports 5 package managers: pacman (Arch), apt (Debian/Ubuntu), dnf (Fedora), zypper (openSUSE), apk (Alpine)
- Fuzzy package search
- Install / Remove / Show Info / System Update
- Auto-detects available package manager
- Safe package name handling via shell escaping

#### Requirements

- One of: `pacman`, `apt-get`, `dnf`, `zypper`, or `apk`

#### Configuration

- `icon` — icon name (default: `software-install`)
- `min_score` — minimum fuzzy score (default: `20`)