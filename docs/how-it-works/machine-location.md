# Where a machine is

Every machine has a `location`: where it is, such as `hostinger`, `home`,
`office` or `bedroom`. It changes nothing about how the CLI reaches the
machine. It is there so a view of your machines — the macOS app's network
map, or your own script reading `machines list --format json` — can group
them by place.

## Why empty means `external`

Every machine added before this field existed has none. Treating "none"
as a place of its own, `external`, keeps those configurations valid and
gives every machine an answer without rewriting anyone's `config.yml`.
`config show --format json` and `machines list --format json` always print
the effective value, so a reader never has to know this default.

`external` is also what `machines add` offers, because a server you add is
most often one you rent. Picking that default writes nothing, since
nothing already means `external`.

## Why `create-local` writes `local`, and `self` does not

Your own computer (`self: true`) can only ever be here, so its default is
worked out, not stored: a self machine with no `location` is `local`.

A `create-local` VM is a normal machine in `config.yml` — an address, a
port, a key. Nothing in that entry says it lives on this computer, so
`create-local --add` writes `location: local` into it. A VM added before
this release has no `location` and shows as `external`; set it with
`devmachine machines edit <name> --location local`.

## Why the value is checked

The value is trimmed and lowercased, so `Home` and `home ` are one place,
not two groups on a map. Only letters, numbers, spaces, dots, underscores
and hyphens are allowed, up to 40, so it is always safe to show as a label.
