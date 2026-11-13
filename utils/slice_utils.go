package utils

// SliceWhere provides a way of querying specific elements which fit some criteria into a new slice.
func SliceWhere[T any](x []T, f func(x T) bool) []T {
	r := make([]T, 0)
	for i := 0; i < len(x); i++ {
		if f(x[i]) {
			r = append(r, x[i])
		}
	}
	return r
}
