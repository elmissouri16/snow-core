package update

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Version is the release format published by Snow.
type Version struct {
	Major, Minor, Patch uint64
	Alpha               *uint64
}

func ParseVersion(value string) (Version, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return Version{}, errors.New("update: invalid empty or padded version")
	}
	value, _ = strings.CutPrefix(value, "v")
	base, prerelease, hasPrerelease := strings.Cut(value, "-")
	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("update: invalid release version %q", value)
	}
	numbers := make([]uint64, 3)
	for i, part := range parts {
		if part == "" || len(part) > 1 && part[0] == '0' {
			return Version{}, fmt.Errorf("update: invalid release version %q", value)
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return Version{}, fmt.Errorf("update: invalid release version %q", value)
			}
		}
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return Version{}, fmt.Errorf("update: invalid release version %q", value)
		}
		numbers[i] = n
	}
	v := Version{Major: numbers[0], Minor: numbers[1], Patch: numbers[2]}
	if !hasPrerelease {
		return v, nil
	}
	alpha, ok := strings.CutPrefix(prerelease, "alpha.")
	if !ok || alpha == "" || len(alpha) > 1 && alpha[0] == '0' {
		return Version{}, fmt.Errorf("update: unsupported release version %q", value)
	}
	for _, r := range alpha {
		if r < '0' || r > '9' {
			return Version{}, fmt.Errorf("update: unsupported release version %q", value)
		}
	}
	n, err := strconv.ParseUint(alpha, 10, 64)
	if err != nil || n == 0 {
		return Version{}, fmt.Errorf("update: invalid release version %q", value)
	}
	v.Alpha = new(n)
	return v, nil
}

func (v Version) String() string {
	base := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Alpha != nil {
		return fmt.Sprintf("%s-alpha.%d", base, *v.Alpha)
	}
	return base
}

func Compare(a, b Version) int {
	for _, pair := range [][2]uint64{{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if a.Alpha == nil && b.Alpha != nil {
		return 1
	}
	if a.Alpha != nil && b.Alpha == nil {
		return -1
	}
	if a.Alpha == nil {
		return 0
	}
	if *a.Alpha < *b.Alpha {
		return -1
	}
	if *a.Alpha > *b.Alpha {
		return 1
	}
	return 0
}
