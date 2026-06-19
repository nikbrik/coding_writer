package contains
import (
	"testing"
)

func TestContainsDuplicate(t *testing.T) {
	tests := []struct {
		name string
		nums []int
		want bool
	}{
		{"empty slice", []int{}, false},
		{"single element", []int{1}, false},
		{"duplicate positive", []int{1, 2, 3, 1}, true},
		{"duplicate negative", []int{-1, -2, -3, -1}, true},
		{"no duplicates", []int{1, 2, 3, 4}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContainsDuplicate(tt.nums); got != tt.want {
				t.Errorf("ContainsDuplicate() = %v, want %v", got, tt.want)
			}
		})
	}
}
