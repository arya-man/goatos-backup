package postgres

// parkQualifiedPen puts the park code in front of a pen's name, "CPT · Castro 1", the one way the
// Counts pages name a pen when two farms can be on screen together. 66 of 154 real shed names exist
// in both farms, so "Castro 1" alone does not say which farm's pen it is. With no park code, or no
// pen name, the pen name is returned unchanged rather than a dangling separator.
func parkQualifiedPen(parkCode, pen string) string {
	if parkCode == "" || pen == "" {
		return pen
	}
	return parkCode + " · " + pen
}
