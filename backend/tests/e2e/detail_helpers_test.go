package e2e

func detailString(value *string) string {
	if value == nil {
		return "nil"
	}
	return *value
}
