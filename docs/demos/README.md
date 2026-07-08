# Demos

This is a self-contained ultra project whose only purpose is generating the CLI
demo gifs under docs/assets/demos. The gifs are checked in; this directory is
how they are produced, so a change to the CLI can be reflected by re-rendering
rather than re-recording by hand.

## Layout

The project is a small module of its own, kept separate from the root module so
it never enters the CLI's build, test, or lint. It builds against the ultra
source in this repo through a relative replace in go.mod.

- main.go registers an `env` secret resolver that reads secrets from environment
  variables, so the demos run offline with no real secret store.
- apps/worker/config/config.go is the sample Config the demos show.
- docker-compose.yml and .ultra.toml round out a realistic single-app setup.
- tapes/ holds one vhs tape per command; each renders to docs/assets/demos.

## Regenerating

vhs is pinned in mise.toml. Its runtime dependencies, ttyd and ffmpeg, are not
available for macOS through mise, so install them with the system package
manager (`brew install ttyd ffmpeg` on macOS, apt on Linux).

```bash
mise install
brew install ttyd ffmpeg
mise run gen-docs --demos
```

Or render directly:

```bash
docs/demos/generate.sh
```
