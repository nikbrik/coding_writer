package main

func IsValid(s string) bool {
	if len(s)%2 != 0 {
		return false
	}
	stack := []rune{}
	mapping := map[rune]rune{')': '(', ']': '[', '}': '{'}
	for _, char := range s {
		if opener, ok := mapping[char]; ok {
			if len(stack) == 0 || stack[len(stack)-1] != opener {
				return false
			}
			stack = stack[:len(stack)-1]
		} else {
			stack = append(stack, char)
		}
	}
	return len(stack) == 0
}
