// Package diag is the vocabulary of codes for refusals and warnings, the Fault a
// refusal travels as from where it is raised, and the stderr stream that prints it.
package diag

// Code names what the caller does next, not what broke. The values are a contract,
// added to and never renamed; when each is raised is ADR-0005's table.
type Code string

const (
	BadUsage        Code = "bad_usage"
	UnknownName     Code = "unknown_name"
	MissingRequired Code = "missing_required"
	NotFound        Code = "not_found"
	Denied          Code = "denied"
	Rejected        Code = "rejected"
	UpstreamFailed  Code = "upstream_failed"
	UpstreamLied    Code = "upstream_lied"
	WriteUncertain  Code = "write_uncertain"
)
