package cli_test

func unknownEntry(field string, nearest ...any) []detail {
	return []detail{{"field", field}, {"nearest", append([]any{}, nearest...)}}
}

func missingEntry(field string, serverTypeOrNil any) []detail {
	return []detail{{"field", field}, {"type", serverTypeOrNil}}
}
