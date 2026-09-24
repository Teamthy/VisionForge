# Contributing

1. Fork and branch off `main` using conventional-commit prefixes (`feat:`, `fix:`,
   `refactor:`, `docs:`, `test:`, `chore:`, `security:`, `perf:`).
2. Keep commits focused; avoid giant all-in-one commits.
3. Run formatters, linters, and tests before opening a PR:
   - `make fmt`
   - `make lint`
   - `make test`
4. For new API endpoints, update the OpenAPI spec and documentation.
5. Add tests for any new state transitions, auth logic, or retry behavior.
6. Document architectural decisions in `docs/decisions/ADR-xxx-*.md`.
7. Security-sensitive changes must reference the threat model in `docs/security/SECURITY.md`.
