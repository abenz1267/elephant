### Elephant System Power

Provides system power actions: lock, logout, suspend, hibernate, reboot, and shutdown.

#### Features

- WM-aware lock/logout: auto-detects hyprlock/swaylock/hyprctl/swaymsg, falls back to loginctl
- Hibernate detection: only shows hibernate option when swap supports it
- Configurable lock/logout commands with auto-detection
- No external dependencies beyond systemd

#### Requirements

- `systemctl` (systemd)

#### Configuration

- `lock_cmd` — override lock command (default: auto-detected)
- `logout_cmd` — override logout command (default: auto-detected)
- `auto_detect_lock` — auto-detect lock command (default: `true`)
- `auto_detect_logout` — auto-detect logout command (default: `true`)
- `icon` — icon name (default: `system-shutdown`)