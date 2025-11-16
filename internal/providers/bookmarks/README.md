### Elephant Bookmarks

URL bookmark manager

#### Features

- create / remove bookmarks
- import bookmarks from installed browsers
- cycle through categories
- customize browsers and set per-bookmark browser
- git integration (requires ssh access)

#### Requirements

- `jq` for importing from chromium based browsers
- `sqlite3` for importing from firefox based browsers

#### Git Integration

You can set

```toml
location = "https://github.com/abenz1267/elephantbookmarks"
```

This will automatically try to clone/pull the repo. It will also automatically comimt and push on changes.

#### Usage

##### Adding a new bookmark

By default, you can create a new bookmark whenever no items match the configured `min_score` threshold. If you want to, you can also configure `create_prefix`, f.e. `add`. In that case you can do `add:bookmark`.

URLs without `http://` or `https://` will automatically get `https://` prepended.

Examples:

```
example.com                       -> https://example.com
github.com GitHub                 -> https://github.com (with title "Github")
add reddit.com Reddit             -> https://reddit.com (with title "Reddit")
w:work-site.com                   -> https://work-site.com (in "work" category)
```

##### Categories

You can organize bookmarks into categories using prefixes:

```toml
[[categories]]
name = "work"
prefix = "w:"

[[categories]]
name = "personal"
prefix = "p:"
```

##### Browsers

You can customize browsers used for opening bookmarks like this:

```toml
[[browsers]]
name = "Zen"
command = "zen-browser"

[[browsers]]
name = "Chromium"
command = "chromium"

[[browsers]]
name = "Chromium App"
command = "chromium --app=%VALUE%"
```
