package dataacquisition

// derefInt32 returns the pointed value or 0.
func derefInt32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}
