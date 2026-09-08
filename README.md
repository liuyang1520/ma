# ma

A Markdown pager with actual heading sizes, proportional fonts, and GitHub-style layout in your terminal.

```sh
ma README.md
cat notes.md | ma
```

`ma` is a single Go executable with its own Markdown layout and painting engine. It uses Goldmark to parse GitHub Flavored Markdown, Go's OpenType rasterizer to draw font outlines, and the Kitty graphics protocol for display. **Chrome/Chromium is no longer required.** There are no browser processes, HTML pages, or external rendering commands.

The engine computes a reusable document layout, then paints nearby content into a native RGBA canvas. The terminal caches the pixels, so nearby scrolling changes the displayed crop. At the default size, the six heading levels are **48, 36, 30, 24, 21, and 20.4 px**, over a **24 px** body font. Typography, line spacing, rules, tables, task lists, blockquotes, and inline code follow GitHub's visual conventions. All styling rules are implemented in Go: layout assigns font sizes, weights, spacing, and borders; theme palettes supply colors; the painter draws the resulting glyphs and shapes. The renderer implements Markdown-specific layout rather than a general CSS engine, so it is an approximation of GitHub's appearance.

Go fonts are bundled in the executable. On macOS, the renderer prefers Arial for proportional text and Menlo for code; on Linux, it tries Liberation Sans and Mono. A locally installed Arial Unicode or Noto Sans CJK font provides additional character coverage when available. Nothing is downloaded at runtime.

Bundled font and library licenses are included in [THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt).

## Install

Download a prebuilt binary from [Releases](https://github.com/liuyang1520/ma/releases). Packages are available for macOS and Linux on **arm64** and **amd64**. Each archive includes `ma`, this README, the MIT license, and third-party notices. Linux binaries are built with CGO disabled and do not need a system C library.

The optional installer detects your platform, downloads the pinned **v0.2.2** archive over HTTPS, verifies its SHA-256 checksum, and installs to `~/.local/bin/ma`. Download it, review it, then run it:

```sh
curl --disable --proto '=https' --proto-redir '=https' --tlsv1.2 -fL \
  -o install-ma.sh https://github.com/liuyang1520/ma/releases/download/v0.2.2/install.sh
less install-ma.sh
sh install-ma.sh
```

Ensure `~/.local/bin` is on your `PATH`, then run `ma file.md`. Use `sh install-ma.sh --prefix /absolute/path` for a different prefix. The installer needs standard shell utilities, `curl`, `tar`, and either `sha256sum` or `shasum`. It does not invoke `sudo`, install dependencies, execute the downloaded binary, or modify shell configuration. A successful install replaces an existing `ma` atomically and places license notices in `PREFIX/share/doc/ma`.

`SHA256SUMS` covers all release archives and the installer. Checksums detect corrupt or mismatched downloads; they are not signatures, and installation still trusts this GitHub repository and its release assets. The macOS builds are not notarized. The seven-day policy applies to third-party build dependencies; installing a newly published `ma` release does not download any dependencies.

## Build from source

Build requirements: macOS or Linux, Go 1.26+, and Python 3.10+ for dependency checks. The installed binary only requires the operating system and a terminal implementing [Kitty graphics](https://sw.kovidgoyal.net/kitty/graphics-protocol/), such as [Ghostty](https://ghostty.org/docs/features) or Kitty. PNG export also works without a terminal. No Go, Python, browser, or installed font package is required to run the binary.

```sh
make deps       # verify release age, then download pinned dependencies
make build      # offline; produces bin/ma
make test
make install    # installs ~/.local/bin/ma; ensure this directory is on PATH
ma examples/showcase.md
```

Use `make install PREFIX=/another/prefix` to change the install location. Install replaces an existing `ma` at that location. The former `--browser` option has been removed and `MA_BROWSER` is no longer used. `ma` never installs packages or updates itself at runtime. The project is [MIT licensed](LICENSE); bundled dependencies retain their respective licenses.

Options must precede the filename:

```sh
ma --theme light README.md
ma --font-size 28 README.md
ma --scale 2 README.md
ma --export preview.png --width 1000 --height 800 README.md
ma -- -filename-starting-with-a-dash.md
```

Export saves the first viewport, with dimensions in logical pixels. The default export scale is 1; at scale 2, each logical pixel occupies two physical pixels in each dimension. Glyph outlines are rasterized at that scale. In the interactive pager, scale is inferred from terminal pixel/cell dimensions; `--scale` overrides it. `--force-graphics` bypasses capability detection for terminals whose query responses are unavailable.

## Rendering and scrolling

The interactive path sends **losslessly compressed RGBA pixels**, using Kitty's `f=32,o=z` format. It does not PNG-encode each frame. PNG is still available for explicit file exports.

The pager keeps two rendered bands in terminal memory, normally up to three viewport heights each. Scrolling inside a band sends only an `a=p` placement with a new source rectangle. This avoids glyph rasterization, compression, image upload, and image decoding for each movement. When a new band is needed, it is uploaded to the hidden image before replacing the visible placement. Resize, zoom, theme, reload, and search changes invalidate the bands. Ctrl-L forces fresh pixels if a terminal's cache is cleared.

Each cached band is limited to 16 megapixels, except for viewports already larger than that, where the band is just one viewport. There is no full-document bitmap. First load, distant jumps, and cache refills still incur rendering and transfer costs; terminal GPU latency and SSH throughput can affect responsiveness.

The local cached-scroll benchmark at an 800×600 logical viewport measured:

| Image update | Scale 1 | Scale 2 |
| :--- | ---: | ---: |
| Previous full PNG: preparation | 8.9 ms | 26.5 ms |
| Previous full PNG: wire bytes | 105,637 | 214,880 |
| Cached crop: preparation | 0.23 µs | 0.23 µs |
| Cached crop: wire bytes | 69 | 71 |

These measurements exclude the initial cache fill, status line, actual TTY writes, and the terminal's GPU work. They quantify the work eliminated for a cache hit, not end-to-end display latency. Run `make bench` to reproduce them on your machine.

## Controls

| Action | Keys |
| :--- | :--- |
| Scroll one line | `j` / `k`, ↓ / ↑, Enter |
| Scroll with mouse | Wheel |
| Page down / up | Space / `b`, Page Down / Page Up, Ctrl-F / Ctrl-B |
| Half page down / up | `d` / `u`, Ctrl-D / Ctrl-U |
| Top / bottom | `g` / `G`, Home / End |
| Search rendered text | `/`, type query, Enter |
| Next / previous match | `n` / `N` |
| Cancel or clear search | Escape |
| Increase / decrease font size | `+` / `-` |
| Toggle dark / light | `t` |
| Reload file from disk | `r` |
| Show key hints | `?` |
| Redraw | Ctrl-L |
| Quit | `q`, Ctrl-C |

The pager uses an alternate screen, detects terminal resizing, and restores terminal input, cursor, mouse mode, and its own graphics placement when exiting normally or receiving SIGINT, SIGTERM, or SIGHUP.

## Dependency policy

Every direct, transitive, and development dependency must be published for **at least seven complete days** before its source is downloaded or installed. The policy also applies to any additional tools introduced during development.

`scripts/deps.py` is a standard-library-only Go proxy that checks version metadata **before serving either a manifest or source archive**. It requires an upstream GitHub publication timestamp or a matching historical entry in the [public Go module index](https://index.golang.org/), in addition to an old enough module timestamp. An old commit alone does not establish release age. Index hints in `dependencies.json` are verified against the public index on every check. Unavailable metadata, too-recent releases, and unverified publication ages fail closed.

The checker audits the full selected graph even when modules are cached. Exact versions are pinned in `go.mod`; `go.sum` provides Go checksum verification. `.cache/dependency-audit.json` records each successful check. Builds and tests use `GOPROXY=off`, `-mod=readonly`, and `GOTOOLCHAIN=local` to prevent incidental downloads and automatic toolchain installation.

To change a dependency, edit its exact version in `go.mod`, then run:

```sh
python3 scripts/deps.py tidy
make deps
make test
```

Do not use `go get`, `@latest`, replacement directives, or an alternate proxy to bypass this workflow. For a release with no GitHub release record, the checker searches a bounded window of the index using the registry's object timestamp as a hint. If it cannot locate an older version, find its index entry and add its exact timestamp to `dependencies.json`; the checker independently validates that entry. Seven days of age is a quarantine policy, not a vulnerability audit.

## Current scope

- CommonMark plus GFM tables, task lists, strikethrough, and autolinks. Syntax highlighting, math, Mermaid, and GitHub-specific alerts are not implemented.
- Local PNG/JPEG/GIF images are drawn from within the Markdown file's directory. Path traversal and symlinks escaping that directory are rejected. Images occupy their own line and GIFs show their first frame. Remote/unavailable images show their alt text; SVG and raw HTML are omitted. The renderer performs no network requests and executes no document code.
- Each document is limited to 16 MiB of Markdown, one million laid-out characters, and 200,000 drawing operations. Individual images are limited to 8 MiB / 32 megapixels, with a 128 MiB decoded-image budget. Each viewport is limited to 32 megapixels.
- Rasterized text cannot be selected/copied with terminal selection, and links cannot be clicked. Case-insensitive search works across inline formatting and soft line wraps within a block.
- Latin, Greek, and Cyrillic use the available font glyphs. CJK coverage depends on an installed fallback font. Complex-script shaping, bidirectional layout, ligatures, and color emoji are not implemented; missing glyphs show the font's replacement box.
- One document at a time. Wide code lines and table cells wrap. No watch mode, Windows support, or tmux passthrough yet. Direct Kitty transfer works over SSH using only the `ma` binary on the remote host, though image bandwidth affects responsiveness.
- PNG rendering and terminal protocol behavior are covered by tests. Actual Ghostty/Kitty GPU display still needs a manual check on the terminal you use.

## Verification

```sh
make test           # native layout/raster, input decoding, Kitty chunking, age policy
make integration    # scroll, search, resize, piped input, PTY cleanup
```

Rendering tests check heading sizes, image dimensions, themes, GFM structures, line wrapping, search across styles, local-image boundaries, viewport limits, and cancellation. Cache tests verify that a crop matches the original viewport pixels, nearby scrolls contain no image uploads, fractional-scale crops stay in bounds, and content changes invalidate the cache. Integration tests use a pseudo-terminal emulating Kitty's capability reply, with the child's PATH pointing to a nonexistent directory. They decode compressed RGBA transfers, apply cached crops, and check navigation, piped Markdown, search, resizing, signal cleanup, and unsupported-terminal errors. A repeated-scroll case verifies twelve displayed views from one upload. Generated previews live in `artifacts/`.

Installer tests use local download fixtures to check supported platforms, checksum failures, malformed archives, and preservation of an existing installation on failure.

## Preparing a release

Update the version in `cmd/ma/main.go`, `install.sh`, and the installation instructions together. Run `make deps` to recheck dependency publication ages, followed by `make test` and `make integration`. `make release` cross-compiles all four targets offline into `dist/vVERSION/` and creates deterministic archives, a copy of the installer, and `SHA256SUMS`. It uses the existing Go toolchain and cached modules; it never installs release tools or updates dependencies.

Commit and tag the verified source before publishing. Upload the complete `dist/vVERSION/` contents to a draft GitHub release, verify the uploaded assets against the local checksums, then publish the release. Release artifacts, local caches, and generated previews are ignored by Git.
