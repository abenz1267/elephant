### Elephant WiFi

WiFi network management. Scan, connect, disconnect, and forget networks.

To connect to a new secured network, type `#yourpassword` in the search bar before connecting.

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
- `subtext_format` string - Format string for the subtext displayed under each network
  - `{lock}` - security icon: 🔓 (secured + saved), 🔒 (secured), 🌐 (open)
  - `{status}` - connection status: `Connected`, `Saved`, or empty
  - `{signal}` - signal strength percentage (e.g. `80%`)
  - `{frequency}` - frequency band (e.g. `5 GHz`)
  - `{security}` - security type (e.g. `WPA2`)
