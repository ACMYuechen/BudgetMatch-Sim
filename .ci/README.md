# Local CI

Run the complete CI suite from the repository root:

```bash
.ci/scripts/ci.sh
```

The host needs Docker, Go (the version declared by `go.mod`), Node.js 20, and npm.
Each check can also be run independently through the scripts in `.ci/scripts/`.
The Go check starts temporary etcd and pgvector containers when `ETCD_HOSTS` and
`RAG_TEST_PG_DSN` are not already set.
