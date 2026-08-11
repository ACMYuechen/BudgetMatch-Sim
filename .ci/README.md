# Local CI

Run the complete CI suite from the repository root:

```bash
.ci/scripts/ci.sh
```

The host needs Docker, Go (the version declared by `go.mod`), Node.js 20, and npm.
Each check can also be run independently through the scripts in `.ci/scripts/`.
The Go check starts temporary etcd and pgvector containers when `ETCD_HOSTS` and
`RAG_TEST_PG_DSN` are not already set.

Pull request and push workflows first run `detect-changes.sh`, then select the
Go, web, security, and container jobs from the complete base-to-head diff.
Changes to `.github/` or `.ci/`, manual workflow runs, initial branch pushes,
and unclassified paths use the safe fallback of running the complete suite.
Documentation-only changes skip the four expensive jobs.

Container checks build every image by default. To validate selected images
locally, provide a space-separated target list:

```bash
CI_IMAGE_TARGETS="mall-rpc app" .ci/scripts/container-check.sh
```

Supported targets are `auth-rpc`, `seckill-rpc`, `mall-rpc`, `agent-rpc`,
`payment-rpc`, `app`, `admin`, and `web-ui`.
