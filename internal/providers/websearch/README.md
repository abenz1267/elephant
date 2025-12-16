# Elephant Websearch Provider

Search the web with custom defined search engines.

## Features

- Opening URLs directly
- Define custom search engines
- Search with custom prefixes
- Auto complete suggestions from any JSON API
- Engine finder to search configured engines

## Example Config

```toml
text_prefix = ""
max_api_items = 10
engine_finder_default = false

# name and url are required
# name must be unique
[[entries]]
name = "Example"
url = "https://www.example.com/search?q=%TERM%"

[[entries]]
default = true
name = "DuckDuckGo"
icon = "duckduckgo"
prefix = "!d"
url = "https://duckduckgo.com/?q=%TERM%"
suggestions_url = "https://ac.duckduckgo.com/ac/?q=%TERM%"
suggestions_path = "#.phrase"

[[entries]]
default = false
name = "Google"
icon = "google"
prefix = "!g"
url = "https://www.google.com/search?q=%TERM%"
suggestions_url = "https://suggestqueries.google.com/complete/search?client=firefox&q=%TERM%"
suggestions_path = "1"
```

### Tips

1.  The engine URL and suggestions API don't need to match

```toml
[[entries]]
name = "Crunchyroll"
prefix = "!anime"
url = "https://www.crunchyroll.com/search?q=%TERM%"
# Suggestions from MyAnimeList for Crunchyroll search
suggestions_url = "https://myanimelist.net/search/prefix.json?type=all&keyword=%TERM%&v=1"
suggestions_path = "categories.#(type==\"anime\").items.#.name"
```

2. Give multiple engines the same prefix to query them simultaneously

```toml
[[entries]]
name = "Amazon"
icon = "amazon"
prefix = "!shop"
url = "https://www.amazon.ca/s?k=%TERM%";

[[entries]]
name = "Newegg"
prefix = "!shop"
url = "https://www.newegg.ca/p/pl?d=%TERM%"
suggestions_url = "https://www.newegg.ca/api/SearchKeyword?CountryCode=CAN&keyword=%TERM%&nodeId=-1&from=www.newegg.ca"
suggestions_path = "suggestion.keywords.#.keyword"
```
