package stock

import "testing"

func TestMaxProfit(t *testing.T) {
	tests := []struct {
		name   string
		prices []int
		want   int
	}{
		{"normal case", []int{7, 1, 5, 3, 6, 4}, 5},
		{"increasing", []int{1, 2, 3, 4, 5}, 4},
		{"decreasing", []int{7, 6, 4, 3, 1}, 0},
		{"single day", []int{5}, 0},
		{"empty", []int{}, 0},
		{"repeated prices", []int{3, 3, 3}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaxProfit(tt.prices); got != tt.want {
				t.Errorf("MaxProfit() = %v, want %v", got, tt.want)
			}
		})
	}
}
