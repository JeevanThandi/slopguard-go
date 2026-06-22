# Sample apps

Reference fixtures used to regression-test the slopguard-go analyzer. They are
their own Go modules (separate `go.mod`) so the parent module's `go test ./...`
ignores them, and they're excluded from a top-level scan by the default
`**/sampleapps/**` glob — analyze one on demand with an explicit `--path`.

## todolist

A small, fully-tested in-memory todo store. CI asserts its wCRAP report stays
stable:

```bash
slopguard-go analyze --path ./sampleapps/todolist --json --quiet \
  | jq '{methods: .summary.methodCount, crappy: .summary.crappyMethodCount}'
# => { "methods": 10, "crappy": 0 }
```

Clean code, high coverage, zero crappy methods. If those numbers drift without
a deliberate change to the fixture, the analyzer has regressed.
