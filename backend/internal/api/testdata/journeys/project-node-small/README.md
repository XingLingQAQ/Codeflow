# project-node-small

Credential-free, zero-dependency Node fixture for the Codeflow 3.0 journey
E2E tests (plan §21.2).

Scripts — all three are real commands and must exit 0:

- `npm run lint` — `node scripts/lint.js`: local zero-dependency lint rules
  (no `eval(`, no `var`, no trailing whitespace, no tab indent, final
  newline) over `src/` and `test/`.
- `npm run typecheck` — `node --experimental-strip-types --check` on each
  `.ts` file. The sources use erasable TypeScript syntax only, so Node's
  built-in type stripping parses them without any dependency.
- `npm run test` — `node --test` runs `test/greet.test.ts` via `node:test`.

`.env.example` contains placeholder comments only; no real values exist
anywhere in this fixture. Copy the fixture into a temp dir before mutating
it; never run destructive cases against this source tree.
