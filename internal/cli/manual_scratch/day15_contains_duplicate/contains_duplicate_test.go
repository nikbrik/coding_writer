package containsduplicate
import "testing"

func TestContainsDuplicate(t *testing.T) {
	tests := []struct { name string; nums []int; want bool }{
		{"empty", []int{}, false},
		{"single", []int{1}, false},
		{"duplicate positive", []int{1, 2, 1}, true},
		{"duplicate negative", []int{-1, -2, -1}, true},
		{"no duplicate", []int{1, 2, 3}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContainsDuplicate(tt.nums); got != tt.want { t.Fatalf("got %v, want %v", got, tt.want) }
		})
	}
}
