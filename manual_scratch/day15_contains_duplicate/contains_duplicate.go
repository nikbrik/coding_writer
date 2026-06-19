package containsduplicate

// ContainsDuplicate reports whether nums contains the same value at least twice.
// Time: O(n). Space: O(n).
func ContainsDuplicate(nums []int) bool {
	seen := make(map[int]struct{}, len(nums))
	for _, n := range nums {
		if _, ok := seen[n]; ok {
			return true
		}
		seen[n] = struct{}{}
	}
	return false
}
