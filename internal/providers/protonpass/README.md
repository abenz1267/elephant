### Elephant Proton Pass

Access your Proton Pass vaults.

#### Requirements

- `pass-cli` (Proton Pass CLI — <https://protonpass.github.io/pass-cli/>)
- must be logged in (`pass-cli test` exits 0)
- `wl-copy` (for clipboard)

#### Setup

Install and authenticate pass-cli, then add a prefix in your walker config:

```toml
[[providers.prefixes]]
prefix = "*"
provider = "protonpass"
```

#### Actions & keybinds

Add this to your walker config to bind all three actions:

```toml
# ~/.config/walker/config.toml
[providers.actions]
protonpass = [
  { action = "copy_password", label = "copy password", default = true, bind = "Return" },
  { action = "copy_username", label = "copy username", bind = "shift Return" },
  { action = "copy_2fa",      label = "copy 2fa",      bind = "ctrl Return" },
]
```

| Keybind      | Action                                   |
| ------------ | ---------------------------------------- |
| Return       | Copy password                            |
| Shift+Return | Copy username / email                    |
| Ctrl+Return  | Copy TOTP (only shown when item has 2FA) |

#### Configuration

```toml
# ~/.config/elephant/protonpass.toml

vaults = ["Personal"]        # vault names to index; leave empty to use pass-cli default
notify = true                # desktop notification after copying
clear_after = 5              # clear clipboard after X seconds (0 to disable)
```

Multiple vaults:

```toml
vaults = ["Personal", "Work"]
```

#### Walker theme — subtext visibility

By default some walker themes (including omarchy) hide the subtext row entirely.
The subtext shows the email/username and URL for each item. To enable it, add this
to your user theme override (e.g. `~/.config/omarchy/current/theme/walker.css`):

```css
.item-subtext {
  font-size: 13px !important;
  opacity: 0.6 !important;
  min-height: unset !important;
  margin: 0px !important;
  padding: 2px 0 !important;
}
```

#### Notes

- Passwords are **not** cached in memory. Each copy fetches from pass-cli on demand.
- Items are indexed at startup. Restart elephant to pick up new/deleted items.
- Only `Active` login items are indexed; notes, credit cards, aliases and SSH keys are skipped.
