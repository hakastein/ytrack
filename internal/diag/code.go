package diag

type Code string

const (
	BadUsage        Code = "bad_usage"
	UnknownName     Code = "unknown_name"
	MissingRequired Code = "missing_required"
	NotFound        Code = "not_found"
	Denied          Code = "denied"
	Rejected        Code = "rejected"
	UpstreamFailed  Code = "upstream_failed"
	UpstreamInvalid Code = "upstream_invalid"
	WriteUncertain  Code = "write_uncertain"
)
