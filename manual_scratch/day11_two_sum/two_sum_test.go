package main

import (
	"testing"
)

func TestTwoSum(t *testing.T) {
	tests := []struct {
		name        string
		nums        []int
		target      int
		wantFound   bool
	}{
		{"basic", []int{2, 7, 11, 15}, 9, true},
		{"two elements duplicate", []int{3, 3}, 6, true},
		{"duplicates allow multiple valid pairs", []int{3, 2, 4, 3}, 6, true},
		{"negative numbers", []int{-3, 4, 3, 90}, 0, true},
		{"no solution", []int{1, 2, 3}, 7, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TwoSum(tt.nums, tt.target)
			if !tt.wantFound {
				if got != nil {
					t.Fatalf("TwoSum() = %v, want nil", got)
				}
				return
			}
			if len(got) != 2 {
				t.Fatalf("TwoSum() = %v, want two indices", got)
			}
			if got[0] == got[1] || got[0] < 0 || got[1] < 0 || got[0] >= len(tt.nums) || got[1] >= len(tt.nums) {
				t.Fatalf("TwoSum() returned invalid indices %v for len %d", got, len(tt.nums))
			}
			if sum := tt.nums[got[0]] + tt.nums[got[1]]; sum != tt.target {
				t.Fatalf("TwoSum() indices %v sum to %d, want %d", got, sum, tt.target)
			}
		})
	}
}
