### Elephant Todo

Basic Todolist

#### Features

- basic time tracking
- create new scheduled items
- notifications for scheduled items
- mark items as: done, active
- urgent items
- clear all done items

#### Requirements

- `notify-send` for notifications

#### Usage

##### Creating a new item

By default, you can create a new item whenever no items matches the configured `min_score` threshold. If you want to, you can also configure `create_prefix`, f.e. `add`. In that case you can do `add:new item`.

If you want to create a schuduled task, you can prefix your item with f.e.:

```
+5d my task
in 10m my task
in 5d at 15:00 my task
jan 1 at 13:00 my task
january 1 at 13:00 my task
1 jan at 13:00 my task
```

Adding a `!` suffix will mark an item as urgent.
