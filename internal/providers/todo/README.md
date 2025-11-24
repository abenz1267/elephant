### Elephant Todo

Basic Todolist

#### Features

- basic time tracking
- create new scheduled items
- notifications for scheduled items
- mark items as: done, active
- urgent items
- clear all done items
- git integration (requires ssh access)

#### Requirements

- `notify-send` for notifications

#### Git Integration

You can set

```toml
location = "https://github.com/abenz1267/elephanttodo"
```

This will automatically try to clone/pull the repo. It will also automatically comimt and push on changes.

#### Usage

##### Creating a new item

If you want to create a scheduled task, you can prefix your item with f.e.:

```
+5d > my task
in 10m > my task
in 5d at 15:00 > my task
jan 1 at 13:00 > my task
january 1 at 13:00 > my task
1 jan at 13:00 > my task
```

Adding a `!` suffix will mark an item as urgent.

##### Time-based searching

Similar to creating, you can simply search for like `today` to get all items for today.
