// Command lookup by client key: the read half of §20.4's reconciliation
// endpoint (T1.11.b).
//
// commands.go (T1.11.a) reads a command by its *full* scope, which requires the
// operation. The reconciliation endpoint cannot require it: a client that lost
// the response of a write still holds only the Idempotency-Key it generated
// before sending (§20.4), so the server has to find the record by
// (principal, project, command_id) and then answer either the single record or
// a 409 telling the client to name the operation. This file is that lookup, and
// nothing else: it adds no state, no transition and no table.
//
// Why it is a separate file rather than another function in commands.go:
// commands.go is the storage half of the ledger and was frozen when T1.11.a was
// accepted; T1.11.b needs one new read path plus migration 007's index, and
// keeping them together makes the new surface reviewable on its own. Like
// commands.go it takes a Querier, so the caller may read through the pool (the
// HTTP path) or inside its own transaction.
//
// Why a *slice* is returned even though a well-behaved client sends one key per
// operation: §27.3's idempotency scope includes the operation, so one client key
// used for "runs.create" and "runs.cancel" is two legal records, not a
// conflict. Collapsing them here — by returning the first row — would be the
// server guessing which operation the caller meant, and the caller's next
// action differs: with one row it reads a status, with several it must
// disambiguate. The rows are ordered by operation so that answer is stable.
//
// The lookup is scoped by principal and project, so it can never see another
// principal's command even when the client key is identical (the endpoint's
// answer for a foreign key is the same 404 as for a key that was never used,
// which is what keeps the endpoint from confirming that someone else's key
// exists).

package runstore

import (
	"context"
	"fmt"
	"unicode"
)

// FindCommandsByClientKey returns every command record claimed under
// (principalID, projectID, commandID), ordered by operation.
//
// The result is empty (never an error) when the key was never used in this
// scope: "no such command" is an ordinary outcome of §20.4's reconciliation — a
// client may ask about a request whose response it lost before the server
// committed anything — and the API layer answers it as 404 command_not_found.
// An empty slice is therefore not ErrNotFound here; callers that want the
// single-record semantics use GetCommand with the full key.
//
// The three arguments are validated with the same rules as a CommandKey's
// corresponding fields (non-blank, within the byte limits, unchanged by
// trimming; command_id printable ASCII without whitespace, because it travels
// in a URL path). A violation wraps ErrInvalidCommand before any SQL runs, so a
// caller mixing up path segments and keys gets the same refusal it would get
// from ClaimCommandTx rather than an empty answer that looks like "not found".
// The operation is not a parameter: the lookup deliberately does not name one.
func FindCommandsByClientKey(ctx context.Context, q Querier, principalID, projectID, commandID string) ([]CommandRecord, error) {
	if q == nil {
		return nil, fmt.Errorf("%w: nil Querier", ErrInvalidCommand)
	}
	if err := checkCommandField("principal_id", principalID, MaxCommandPrincipalLength); err != nil {
		return nil, err
	}
	if err := checkCommandField("project_id", projectID, MaxCommandProjectLength); err != nil {
		return nil, err
	}
	if err := checkCommandField("command_id", commandID, MaxCommandIDLength); err != nil {
		return nil, err
	}
	for _, r := range commandID {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) || unicode.IsSpace(r) {
			return nil, fmt.Errorf("%w: command_id %q contains a non-printable or non-ASCII character",
				ErrInvalidCommand, commandID)
		}
	}

	// ORDER BY operation makes the multi-row answer stable, and it is the order
	// §20.4's 409 reports the operations in. The (principal, project,
	// command_id) index of migration 007 serves the WHERE clause; the sort is
	// over the handful of rows one client key can name.
	rows, err := q.QueryContext(ctx, `
		SELECT `+commandColumns+`
		FROM command_records
		WHERE principal_id = ? AND project_id = ? AND command_id = ?
		ORDER BY operation`,
		principalID, projectID, commandID,
	)
	if err != nil {
		return nil, fmt.Errorf("find commands by client key %s: %w", commandID, err)
	}
	defer rows.Close()

	var out []CommandRecord
	for rows.Next() {
		record, err := scanCommand(rows)
		if err != nil {
			return nil, fmt.Errorf("scan command %s: %w", commandID, err)
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		// A read failure must not leave a caller with half an answer: the
		// partial slice is dropped and the error returned.
		return nil, fmt.Errorf("iterate commands for %s: %w", commandID, err)
	}
	return out, nil
}
