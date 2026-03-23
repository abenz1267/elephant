### Elephant WiFi

WiFi network management. Scan, connect, disconnect, and forget networks.

Connecting to a new secured network will open a terminal to prompt for the password.

#### Requirements

One of the following utils/backend:

- `nmcli` - Network Manager

#### Configuration

- `backend` string - WiFi backend
  - `auto` - Will try all backend until one works
  - `nm` - Network Manager
- `message_time` int - Seconds to show status messages
  - (e.g. Connecting, Disconnecting)
- `error_time` int - Seconds to show error messages
  - (e.g. Connection failed, Password required)
- `reopen_after_fail` bool - Reopen wifi menu after connection failure (default: `true`)
- `reopen_after_connect` bool - Reopen wifi menu after successful connection (default: `false`)
- `show_password_dots` bool - Show dots while typing password in terminal (default: `true`)
- `subtext_format` string - Format string for the subtext displayed under each network
  - `%LOCK%` - security icon: 🔓 (secured + saved), 🔒 (secured), 🌐 (open)
  - `%STATUS%` - connection status: `Connected`, `Saved`, or empty
  - `%SIGNAL%` - signal strength percentage (e.g. `80%`)
  - `%FREQUENCY%` - frequency band (e.g. `5 GHz`)
  - `%SECURITY%` - security type (e.g. `WPA2`)
