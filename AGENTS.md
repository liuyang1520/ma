# Development policy

- Build `ma` as a Go CLI for macOS and Linux. Own the native Markdown layout and painting engine; do not add a browser runtime. Prioritize actual GitHub-style typography rendered through Kitty graphics.
- Every third-party dependency, including transitive and development dependencies, must be at least 7 × 24 hours old before downloading its source or installing it. Pin exact versions. Never use `@latest`, floating tags, or automatic browser downloads.
- Use `python3 scripts/deps.py download` (or `tidy`) for dependency downloads. It checks the full module graph and gates registry requests before returning manifests or source archives. Use `make build` and `make test` for offline compilation with the existing toolchain.
- Do not bypass the age gate with `go get`, `go install`, an alternative proxy, or a package manager. Verify the publication date of any additional non-Go dependency before installation.
- Keep `go.sum` checked in. Run `make test` and `make integration` when changing rendering. Verify native PNG output visually and retain the 24 px default body size.
