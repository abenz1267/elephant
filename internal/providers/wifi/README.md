### Elephant WiFi

WiFi network management. Scan, connect, disconnect, and forget networks.

Connecting to a new secured network will prompt for the password using the configured authentication agent.

#### Requirements

One of the following utils/backend:

- `nmcli` - Network Manager

#### Password Prompts

When connecting to a new password-protected network, a password prompt is used to request the password:

- `terminal` - Opens a terminal window for password input
- `dmenu` - Uses a dmenu-compatible tool (walker, rofi, wofi)
- `custom` - Uses a custom command defined in `custom_prompt_command`

#### Configuration

- `backend` string - WiFi backend (default: `auto`)
  - `auto` - Will try all backends until one works
  - `nm` - Network Manager
- `password_prompt` string - Password prompt method (default: `dmenu`)
  - `auto` - Will try all prompts until one works
  - `terminal` - Terminal-based password prompt
  - `dmenu` - dmenu-compatible tool
  - `custom` - Custom command
- `dmenu_command` string - dmenu-compatible tool to use (default: `walker`)
  - `auto` - Will try walker, rofi, wofi in order
  - `walker`, `rofi`, `wofi` - Use a specific tool
- `custom_prompt_command` string - Custom command for the `custom` password prompt. Use `%PROMPT%` as placeholder for the prompt text
  - e.g. `rofi -dmenu -password -p %PROMPT%`
- `message_time` int - Seconds to show status messages (default: `1`)
- `error_time` int - Seconds to show error messages (default: `3`)
- `reopen_after_fail` bool - Reopen wifi menu after connection failure (default: `true`)
- `reopen_after_connect` bool - Reopen wifi menu after successful connection (default: `false`)
- `show_password_dots` bool - Show dots while typing password in terminal (default: `true`)
- `notify` bool - Show desktop notifications (default: `true`)
- `subtext_format` string - Format string for the subtext displayed under each network
  - `%LOCK%` - security icon: 🔓 (secured + saved), 🔒 (secured), 🌐 (open)
  - `%STATUS%` - connection status: `Connected`, `Saved`, or empty
  - `%SIGNAL%` - signal strength percentage (e.g. `80%`)
  - `%FREQUENCY%` - frequency band (e.g. `5 GHz`)
  - `%SECURITY%` - security type (e.g. `WPA2`)
