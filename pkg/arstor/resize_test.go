package arstor

import (
	"testing"
)

func TestGetDiskType(t *testing.T) {
	tests := []struct {
		name     string
		disk     string
		output   string
		sep      string
		expected string
	}{
		{
			name:     "test_empty_n",
			disk:     "/dev/loop3",
			output:   "",
			sep:      "\n",
			expected: "",
		},
		{
			name:     "test_old",
			disk:     "/dev/loop3",
			output:   "DEVNAME=/dev/loop3\nTYPE=xfs\n",
			sep:      "\n",
			expected: "xfs",
		},
		{
			name:     "test_new_n",
			disk:     "/dev/loop3",
			output:   "/dev/loop3: UUID=\"b60a3828-ea89-4f6f-b9c8-670ab628f4bb\" TYPE=\"xfs\"\n",
			sep:      "\n",
			expected: "",
		},
		{
			name:     "test_new",
			disk:     "/dev/loop3",
			output:   "/dev/loop3: UUID=\"b60a3828-ea89-4f6f-b9c8-670ab628f4bb\" TYPE=\"xfs\"\n",
			sep:      " ",
			expected: "xfs",
		},
		{
			name:     "test_new_n_space",
			disk:     "/dev/loop3",
			output:   "/dev/loop3: UUID=\"b60a3828-ea89-4f6f-b9c8-670ab628f4bb\" TYPE=\"xfs\"",
			sep:      "\n",
			expected: "",
		},
		{
			name:     "test_new_space",
			disk:     "/dev/loop3",
			output:   "/dev/loop3: UUID=\"b60a3828-ea89-4f6f-b9c8-670ab628f4bb\" TYPE=\"xfs\"",
			sep:      " ",
			expected: "xfs",
		},
		{
			name:     "test_new_noquote_n",
			disk:     "/dev/loop3",
			output:   "/dev/loop3: UUID=b60a3828-ea89-4f6f-b9c8-670ab628f4bb TYPE=xfs\n",
			sep:      "\n",
			expected: "",
		},
		{
			name:     "test_new_noquote",
			disk:     "/dev/loop3",
			output:   "/dev/loop3: UUID=b60a3828-ea89-4f6f-b9c8-670ab628f4bb TYPE=xfs\n",
			sep:      " ",
			expected: "xfs",
		},
		{
			name:     "test_old_partition_n",
			disk:     "/dev/loop0",
			output:   "DEVNAME=/dev/loop0\nPTTYPE=dos\n",
			sep:      "\n",
			expected: "unknown data, probably partitions",
		},
		{
			name:     "test_old_partition",
			disk:     "/dev/loop0",
			output:   "DEVNAME=/dev/loop0\nPTTYPE=dos\n",
			sep:      " ",
			expected: "",
		},
		{
			name:     "test_new_partition_n",
			disk:     "/dev/loop0",
			output:   "/dev/loop0: UUID=61a18521-3b84-4d20-ac35-0d3f819ce6b8 PTTYPE=dos\n",
			sep:      "\n",
			expected: "",
		},
		{
			name:     "test_new_partition",
			disk:     "/dev/loop0",
			output:   "/dev/loop0: UUID=61a18521-3b84-4d20-ac35-0d3f819ce6b8 PTTYPE=dos\n",
			sep:      " ",
			expected: "unknown data, probably partitions",
		},
		{
			name:     "test_bad_data_n",
			disk:     "/dev/loop3p1",
			output:   "bad data\n",
			sep:      "\n",
			expected: "",
		},
		{
			name:     "test_bad_data",
			disk:     "/dev/loop3p1",
			output:   "bad data\n",
			sep:      " ",
			expected: "",
		},
	}
	for _, test := range tests {
		result := getDiskType(test.disk, test.output, test.sep)
		if result != test.expected {
			t.Errorf("test %q failed: actually %v expected %v ", test.name, result, test.expected)
		}

	}
}
