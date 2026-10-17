# Demo

`demo.sh` onboards a fictitious merchant and has a shopping agent buy from it, with readable output.

```bash
make infra-up && make dev-up      # from the repo root
docs/demo/demo.sh                 # PACE=0 to skip the pauses
```

Set `STORE_ID` / `SKU` if the script cannot parse them from the onboard output.

## Recording with asciinema

```bash
# one-time: pip install asciinema   (or: brew install asciinema)
asciinema rec docs/demo/demo.cast --cols 100 --rows 30 --command docs/demo/demo.sh
asciinema play docs/demo/demo.cast        # review
asciinema upload docs/demo/demo.cast      # prints the public URL
```

Embed in the README by replacing the placeholder under the tagline with:

```markdown
[![asciicast](https://asciinema.org/a/<ID>.svg)](https://asciinema.org/a/<ID>)
```

Alternatively render a GIF with [agg](https://github.com/asciinema/agg): `agg docs/demo/demo.cast docs/demo/demo.gif`, and commit the GIF.
