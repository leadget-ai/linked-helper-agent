package main

import "strings"

// schemaStatements splits a .schema dump into individually-executable
// statements and drops every CREATE TRIGGER (keeping tables, views and
// indexes). Splitting is trigger-aware: a trigger body is BEGIN … END with
// inner semicolons, so a top-level `;` only ends a CREATE TRIGGER once the last
// token before it is END. Single-quoted string literals are skipped so a `;`
// inside a literal never splits.
func schemaStatements(ddl string) []string {
	var (
		out   []string
		buf   strings.Builder
		inStr bool
	)
	runes := []rune(ddl)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		if inStr {
			buf.WriteRune(c)
			if c == '\'' {
				if i+1 < len(runes) && runes[i+1] == '\'' { // escaped '' inside literal
					buf.WriteRune('\'')
					i++
					continue
				}
				inStr = false
			}
			continue
		}
		switch c {
		case '\'':
			inStr = true
			buf.WriteRune(c)
		case ';':
			if statementComplete(buf.String()) {
				appendStatement(&out, buf.String())
				buf.Reset()
			} else {
				buf.WriteRune(c) // semicolon belongs to a trigger body
			}
		default:
			buf.WriteRune(c)
		}
	}
	appendStatement(&out, buf.String())
	return out
}

// statementComplete reports whether the accumulated text forms a complete
// statement at a top-level semicolon. Non-triggers end at their first `;`; a
// CREATE TRIGGER ends only at the `;` following its terminal END.
func statementComplete(stmt string) bool {
	fields := strings.Fields(strings.ToUpper(stmt))
	if len(fields) < 2 || !(fields[0] == "CREATE" && isTrigger(fields)) {
		return true
	}
	return fields[len(fields)-1] == "END"
}

// isTrigger detects CREATE [TEMP] TRIGGER, allowing the optional modifiers
// sqlite emits between CREATE and TRIGGER.
func isTrigger(upperFields []string) bool {
	for _, f := range upperFields[1:] {
		switch f {
		case "TEMP", "TEMPORARY":
			continue
		case "TRIGGER":
			return true
		default:
			return false
		}
	}
	return false
}

// appendStatement trims and records a statement, dropping blanks and triggers.
func appendStatement(out *[]string, stmt string) {
	trimmed := strings.TrimSpace(stmt)
	if trimmed == "" {
		return
	}
	fields := strings.Fields(strings.ToUpper(trimmed))
	if len(fields) >= 2 && fields[0] == "CREATE" && isTrigger(fields) {
		return
	}
	// Skip sqlite-internal tables (sqlite_stat1/stat4 emitted by ANALYZE):
	// their names are reserved and can't be recreated.
	if createsReservedObject(fields) {
		return
	}
	*out = append(*out, trimmed)
}

// createsReservedObject reports whether the statement is a CREATE TABLE/INDEX/
// VIEW whose target object name begins with the reserved "SQLITE_" prefix.
func createsReservedObject(upperFields []string) bool {
	for i, f := range upperFields {
		switch f {
		case "TABLE", "INDEX", "VIEW":
			name := upperFields[i+1]
			if name == "IF" && i+4 < len(upperFields) { // IF NOT EXISTS <name>
				name = upperFields[i+4]
			}
			return strings.HasPrefix(strings.Trim(name, `"'`), "SQLITE_")
		}
	}
	return false
}
