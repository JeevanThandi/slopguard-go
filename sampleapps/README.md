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

CI also pins its mutation baseline:

```bash
slopguard-go mutate --path ./sampleapps/todolist --json --quiet \
  | jq '{mutants: .summary.mutantCount, killed: .summary.killed, survived: .summary.survived, score: .summary.mutationScore}'
# => { "mutants": 17, "killed": 15, "survived": 0, "score": 100 }
```

The tests kill 15 mutants. One mutant (`id++` → `id--` in `Store.All`) loops
forever and hits the timeout, which counts as killed. One mutant
(`id < s.nextID` → `id <= s.nextID` in `Store.All`) is equivalent: `Add` never
stores an ID at or above `nextID`, so the extra lookup finds nothing. It
carries a `slopguard-ignore-mutant(boundary)` marker. If a mutant survives, a
change to the fixture's tests or to the mutation operators needs review.
