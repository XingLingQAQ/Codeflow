# project-go-small

Credential-free Go fixture for the Codeflow 3.0 journey E2E tests (plan §21.2).

- `mathx.Add` is the single testable function; `go test ./...` must exit 0
  with `CGO_ENABLED=0` and no network access (the module has no external
  dependencies, so no `go.sum` exists on purpose).
- `slug.Normalize` and `ident.Normalize` are the duplicate-name candidate
  pair: the same function name in two packages with deliberately different
  implementations (lowercase+dash vs uppercase+underscore), for guard
  rename/similarity scenarios. They live in separate packages because two
  same-name functions in one package would not compile.

This fixture is an independent Go module (its own `go.mod`); it does not
import any codeflow package. Copy it into a temp dir before mutating it;
never run destructive cases against this source tree.
