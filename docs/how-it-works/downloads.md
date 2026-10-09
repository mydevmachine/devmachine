# How a download lands

`devmachine download` exists so an app, an agent or a person can take a
file off a workspace without knowing how to reach it. Each rule below
serves that.

## It reads as the account the file belongs to

With `--workspace`, the CLI logs in as the workspace's own account, the
same way `upload` and `run --workspace` do. It can read exactly what
that account can read, and no more: nothing reads a workspace's files as
root on its behalf.

## The name stays, and nothing is overwritten

A download keeps its name, because it lands among your own files and you
look for it by that name. When `~/Downloads` already holds one, the new
file gets `-2`, `-3` and so on before the extension, the same counter
`upload` uses on the machine.

The bytes go to a hidden temporary file in the destination first. Only
when the whole file has arrived does the CLI claim the first free name
(a create that fails when the name is taken, so two downloads at the
same moment cannot both take it) and move the file there. A transfer
that fails or is cut never leaves a half file under the real name.

## A folder is one archive

A folder comes back as `<name>.tar.gz`, packed on the machine and
streamed in one piece. One archive is written whole or not at all, the
same as a file; a folder copied file by file could stop half way and
look complete. It also keeps permissions and symbolic links as they
were, and never merges into a folder of the same name you already have.

## A path is never a command

A path is whatever its owner typed, and `'; rm -rf ~; $(id).txt` is a
legal file name. The CLI sends it to the machine encoded in base64, and
the machine decodes it into a variable. The path is never part of a
command line, so nothing in it can run.

## An accent a Mac spelled differently still matches

A letter like `ç` can be stored as one character (composed) or as `c`
plus a separate cedilla (decomposed). Linux file names are almost always
composed; a Mac app's process arguments arrive decomposed, even when the
app had them composed. The bytes differ, so the machine would answer
"does not exist" for a file that is there. When the path as given is
not on the machine, `download` tries its composed form before giving up.

## The connection is the one `run` keeps open

The file streams over the same SSH connection `run` and `upload` reuse,
so many downloads in a row share one login. If that connection drops
after part of the file arrived, the CLI does not try the machine's next
address: the next address would send the whole file again after the
part already written. It stops and says so instead.

## Progress knows the whole size before it starts

With `--progress`, the CLI asks the machine about every path before it
reads a byte of any of them, and prints a line with each size first. A
reader that adds the sizes up has the real total from the start, so its
bar never runs to the end for the first file and then jumps back when
the second one appears.

A folder has no size to give. Its archive is packed while it travels,
and how well it compresses is not known until it ends, so its `total` is
`0` and only `done` grows. The lines go to stderr, so stdout stays the
same list of paths, or the same JSON, with or without the flag.
