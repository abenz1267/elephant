### Elephant Bookmarks

Simple URL bookmark manager

#### Features

- create / remove bookmarks
- cycle through categories

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
