# CI Security Baseline

Captured on 2026-08-10 with Gitleaks v8.28.0, govulncheck v1.6.0,
gosec v2.28.0, and Trivy v0.73.0.

This file is an initial inventory, not an allowlist. No security exception is
configured. Critical findings remain blocking; High findings are reported but
non-blocking according to `docs/requirements/ci.md`.

## Source and dependency scan

- Gitleaks: no leak found.
- gosec (`high` severity, `high` confidence): 0 findings. The scan also reported
  existing Go type errors, so affected SSA analysis was skipped.
- govulncheck: could not complete because the current repository does not compile
  at `services/rpc/mall/internal/logic/orderservice/confirm_payment_logic.go:98`.
- Trivy filesystem Critical: 3 blocking findings:
  - `CVE-2026-33815` and `CVE-2026-33816` in `github.com/jackc/pgx/v5` v5.7.4.
  - `CVE-2026-33186` in `google.golang.org/grpc` v1.74.2.
- Trivy filesystem non-blocking inventory: 20 High, 12 Medium, and 3 Low
  findings. The High total consists of 19 Go dependency findings and the
  non-root-user finding for `web-ui/Dockerfile`.

## Image scan

Six backend images and the Web UI image built successfully during local
validation. `mall-rpc` did not build because of the existing type error above.

- The six successful backend images contain one or three of the blocking
  Critical Go dependency findings listed above.
- The Web UI image has no Critical vulnerability.
- The backend runtime image uses Alpine 3.19, which Trivy reports as end-of-life.

The responsible module owners should triage these findings separately. Any
future exception must add an issue identifier, reason, owner, and expiry date;
the CI implementation does not silently suppress them.
