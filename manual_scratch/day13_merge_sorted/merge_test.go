package merge_sorted

import (
	"reflect"
	"testing"
)

func TestMergeSorted(t *testing.T) {
	tests := []struct {
		name string
		a    []int
		b    []int
		want []int
	}{
		{"both empty", []int{}, []int{}, []int{}},
		{"a empty", []int{}, []int{1, 2}, []int{1, 2}},
		{"b empty", []int{1, 2}, []int{}, []int{1, 2}},
		{"both non-empty", []int{1, 3, 5}, []int{2, 4, 6}, []int{1, 2, 3, 4, 5, 6}},
		{"unequal size", []int{1, 2}, []int{3, 4, 5, 6}, []int{1, 2, 3, 4, 5, 6}},
		{"duplicates", []int{1, 1, 2}, []int{1, 2, 3}, []int{1, 1, 1, 2, 2, 3}},
		{"negative values", []int{-5, -2, 0}, []int{-3, 1}, []int{-5, -3, -2, 0, 1}},
		{"already separated ranges", []int{1, 2, 3}, []int{10, 11}, []int{1, 2, 3, 10, 11}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aCopy := append([]int{}, tt.a...)
			bCopy := append([]int{}, tt.b...)

			got := MergeSorted(tt.a, tt.b)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("MergeSorted() = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(tt.a, aCopy) || !reflect.DeepEqual(tt.b, bCopy) {
				t.Fatal("MergeSorted() mutated input slices")
			}
		})
	}
}
