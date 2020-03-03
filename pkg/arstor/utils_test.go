package arstor

import (
	"testing"
)

func TestContains(t *testing.T) {
	var arr = [3]int{1, 2, 3}
	var sli = []int{1, 2, 3}
	var ma = map[string]int{
		"a": 1,
		"b": 2,
		"c": 3,
	}
	tests := []struct {
		name     string
		obj      interface{}
		target   interface{}
		expected bool
	}{
		{
			name:     "test_slice1",
			obj:      1,
			target:   sli,
			expected: true,
		},
		{
			name:     "test_slice2",
			obj:      0,
			target:   sli,
			expected: false,
		},
		{
			name:     "test_array1",
			obj:      1,
			target:   arr,
			expected: true,
		},
		{
			name:     "test_array2",
			obj:      0,
			target:   arr,
			expected: false,
		},
		{
			name:     "test_map1",
			obj:      "a",
			target:   ma,
			expected: true,
		},
		{
			name:     "test_map1",
			obj:      "d",
			target:   ma,
			expected: false,
		},
	}
	for _, test := range tests {
		result, _ := Contains(test.obj, test.target)
		if result != test.expected {
			t.Errorf("test %q failed: actually %v expected %v ", test.name, result, test.expected)
		}
	}
}
