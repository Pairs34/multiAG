# Contributing

Describe the failing behavior, your OS, IDE version, router wire format, and a minimal synthetic request. Keep credentials, capability URLs, account identifiers, private deployment URLs, and project context out of issues and patches.

Run `go test ./...`, `go vet ./...`, `python3 -m unittest discover -s tests -v`, and `python3 scripts/check_public.py` before submitting. Use `gofmt` for Go changes. Tests should verify routing isolation, protocol behavior, and settings recovery with mock services; do not require real accounts.

Report native platform validation separately from cross-compilation. Document changes to the unsupported IDE contract or tool conversion limits. Submit contributions under the MIT license.
