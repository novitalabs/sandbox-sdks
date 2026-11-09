package core

import (
	"encoding/base64"
	"strconv"
	"strings"
)

// compareVersions compares two dotted version strings.
// It returns -1 if a < b, 0 if a == b, and 1 if a > b.
// Non-numeric suffixes (e.g. "-alpha", "-rc1") sort before the bare version.
func compareVersions(a, b string) int {
	an := parseVersion(a)
	bn := parseVersion(b)

	for i := 0; i < len(an.nums) || i < len(bn.nums); i++ {
		var av, bv int
		if i < len(an.nums) {
			av = an.nums[i]
		}
		if i < len(bn.nums) {
			bv = bn.nums[i]
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}

	// Numeric parts equal: a version with a pre-release suffix is lower.
	switch {
	case an.pre == "" && bn.pre != "":
		return 1
	case an.pre != "" && bn.pre == "":
		return -1
	default:
		return strings.Compare(an.pre, bn.pre)
	}
}

type parsedVersion struct {
	nums []int
	pre  string
}

// parseVersion splits "1.2.3-rc1" into [1 2 3] and "rc1".
func parseVersion(v string) parsedVersion {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))

	pre := ""
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		pre = v[idx+1:]
		v = v[:idx]
	}

	parts := strings.Split(v, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		// Stop at the first non-numeric segment; the rest is a pre-release.
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			if p != "" {
				pre = p + pre
			}
			break
		}
		nums = append(nums, n)
	}

	return parsedVersion{nums: nums, pre: pre}
}

// authenticationHeader builds the Basic auth header used by envd for a given
// user. On envd versions older than 0.4.0 a default user is assumed.
func authenticationHeader(envdVersion string, user string) map[string]string {
	if user == "" && compareVersions(envdVersion, EnvdDefaultUser) < 0 {
		user = DefaultUsername
	}
	if user == "" {
		return map[string]string{}
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(user + ":"))
	return map[string]string{"Authorization": "Basic " + encoded}
}

// toUsername maps the SDK's Username type to a string for headers and queries.
func toUsername(u Username) string {
	return u.Name
}
