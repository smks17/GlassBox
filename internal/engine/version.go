package engine

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"
)

// AppVersion is the GlassBox application version.
var AppVersion = Version{Major: 0, Minor: 1, Patch: 0}

// Version represents a GlassBox release.
type Version struct {
	Major uint64
	Minor uint64
	Patch uint64
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

func (v Version) MarshalText() ([]byte, error) {
	return []byte(v.String()), nil
}

func (v *Version) UnmarshalText(text []byte) error {
	version, ok := parseReleaseVersion(string(text))
	if !ok {
		return ErrSnapshotVersion
	}
	*v = version
	return nil
}

func (v Version) Compare(other Version) int {
	return cmp.Or(
		cmp.Compare(v.Major, other.Major),
		cmp.Compare(v.Minor, other.Minor),
		cmp.Compare(v.Patch, other.Patch),
	)
}

func parseReleaseVersion(version string) (Version, bool) {
	var numbers [3]uint64
	parts := strings.SplitN(version, ".", 4)
	if len(parts) != len(numbers) {
		return Version{}, false
	}
	for i, part := range parts {
		number, err := strconv.ParseUint(part, 10, 64)
		if err != nil || strconv.FormatUint(number, 10) != part {
			return Version{}, false
		}
		numbers[i] = number
	}
	return Version{Major: numbers[0], Minor: numbers[1], Patch: numbers[2]}, true
}
