package semver

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Constraints is one or more constraint that a semantic version can be
// checked against.
type Constraints struct {
	constraints [][]*constraint
}

// NewConstraint returns a Constraints instance that a Version instance can
// be checked against. If there is a parse error it will be returned.
func NewConstraint(c string) (*Constraints, error) {

	// Rewrite the constraint string to convert things like ranges
	// into something the checks can handle.
	for _, rwf := range rewriteFuncs {
		c = rwf(c)
	}

	ors := strings.Split(c, "||")
	or := make([][]*constraint, len(ors))
	for k, v := range ors {
		cs := strings.Split(v, ",")
		result := make([]*constraint, len(cs))
		for i, s := range cs {
			pc, err := parseConstraint(s)
			if err != nil {
				return nil, err
			}

			result[i] = pc
		}
		or[k] = result
	}

	o := &Constraints{constraints: or}
	return o, nil
}

// Check tests if a version satisfies the constraints.
func (cs Constraints) Check(v *Version) bool {
	// loop over the ORs and check the inner ANDs
	for _, o := range cs.constraints {
		joy := true
		for _, c := range o {
			if !c.check(v) {
				joy = false
				break
			}
		}

		if joy {
			return true
		}
	}

	return false
}

var constraintOps map[string]cfunc
var constraintRegex *regexp.Regexp

func init() {
	constraintOps = map[string]cfunc{
		"":   constraintEqual,
		"=":  constraintEqual,
		"!=": constraintNotEqual,
		">":  constraintGreaterThan,
		"<":  constraintLessThan,
		">=": constraintGreaterThanEqual,
		"=>": constraintGreaterThanEqual,
		"<=": constraintLessThanEqual,
		"=<": constraintLessThanEqual,
		"~":  constraintTilde,
		"~>": constraintTilde,
	}

	ops := make([]string, 0, len(constraintOps))
	for k := range constraintOps {
		ops = append(ops, regexp.QuoteMeta(k))
	}

	constraintRegex = regexp.MustCompile(fmt.Sprintf(
		`^\s*(%s)\s*(%s)\s*$`,
		strings.Join(ops, "|"),
		cvRegex))

	constraintRangeRegex = regexp.MustCompile(fmt.Sprintf(
		`\s*(%s)\s*-\s*(%s)\s*`,
		SemVerRegex, SemVerRegex))

	constraintCaretRegex = regexp.MustCompile(`\^` + cvRegex)
}

// An individual constraint
type constraint struct {
	// The callback function for the restraint. It performs the logic for
	// the constraint.
	function cfunc

	// The version used in the constraint check. For example, if a constraint
	// is '<= 2.0.0' the con a version instance representing 2.0.0.
	con *Version

	// When an x is used as part of the version (e.g., 1.x)
	minorDirty bool
	patchDirty bool
}

// Check if a version meets the constraint
func (c *constraint) check(v *Version) bool {
	return c.function(v, c)
}

type cfunc func(v *Version, c *constraint) bool

func parseConstraint(c string) (*constraint, error) {
	m := constraintRegex.FindStringSubmatch(c)
	if m == nil {
		return nil, fmt.Errorf("improper constraint: %s", c)
	}

	ver := m[2]
	minorDirty := false
	patchDirty := false
	if isX(m[3]) {
		ver = "0.0.0"
	} else if isX(strings.TrimPrefix(m[4], ".")) {
		minorDirty = true
		ver = fmt.Sprintf("%s.0.0%s", m[3], m[6])
	} else if isX(strings.TrimPrefix(m[5], ".")) {
		patchDirty = true
		ver = fmt.Sprintf("%s%s.0%s", m[3], m[4], m[6])
	}

	con, err := NewVersion(ver)
	if err != nil {

		// The constraintRegex should catch any regex parsing errors. So,
		// we should never get here.
		return nil, errors.New("constraint Parser Error")
	}

	cs := &constraint{
		function:   constraintOps[m[1]],
		con:        con,
		minorDirty: minorDirty,
		patchDirty: patchDirty,
	}
	return cs, nil
}

// Constraint functions
func constraintEqual(v *Version, c *constraint) bool {
	return v.Equal(c.con)
}

func constraintNotEqual(v *Version, c *constraint) bool {
	return !v.Equal(c.con)
}

func constraintGreaterThan(v *Version, c *constraint) bool {
	return v.Compare(c.con) == 1
}

func constraintLessThan(v *Version, c *constraint) bool {
	return v.Compare(c.con) == -1
}

func constraintGreaterThanEqual(v *Version, c *constraint) bool {
	return v.Compare(c.con) >= 0
}

func constraintLessThanEqual(v *Version, c *constraint) bool {
	return v.Compare(c.con) <= 0
}

// ~*, ~>* --> >= 0.0.0 (any)
// ~2, ~2.x, ~2.x.x, ~>2, ~>2.x ~>2.x.x --> >=2.0.0, <3.0.0
// ~2.0, ~2.0.x, ~>2.0, ~>2.0.x --> >=2.0.0, <2.1.0
// ~1.2, ~1.2.x, ~>1.2, ~>1.2.x --> >=1.2.0, <1.3.0
// ~1.2.3, ~>1.2.3 --> >=1.2.3, <1.3.0
// ~1.2.0, ~>1.2.0 --> >=1.2.0, <1.3.0
func constraintTilde(v *Version, c *constraint) bool {
	fmt.Println(v.String(), c.con.String())

	if v.LessThan(c.con) {
		return false
	}

	if v.Major() != c.con.Major() {
		return false
	}

	if v.Minor() != c.con.Minor() && !c.minorDirty {
		return false
	}

	return true
}

type rwfunc func(i string) string

var constraintRangeRegex *regexp.Regexp
var rewriteFuncs = []rwfunc{
	rewriteRange,
	rewriteCarets,
}

const cvRegex string = `v?([0-9|x|X|\*]+)(\.[0-9|x|X|\*]+)?(\.[0-9|x|X|\*]+)?` +
	`(-([0-9A-Za-z\-]+(\.[0-9A-Za-z\-]+)*))?` +
	`(\+([0-9A-Za-z\-]+(\.[0-9A-Za-z\-]+)*))?`

func isX(x string) bool {
	l := strings.ToLower(x)
	return l == "x" || l == "*"
}

func rewriteRange(i string) string {
	m := constraintRangeRegex.FindAllStringSubmatch(i, -1)
	if m == nil {
		return i
	}
	o := i
	for _, v := range m {
		t := fmt.Sprintf(">= %s, <= %s", v[1], v[11])
		o = strings.Replace(o, v[0], t, 1)
	}

	return o
}

// ^ --> * (any)
// ^2, ^2.x, ^2.x.x --> >=2.0.0 <3.0.0
// ^2.0, ^2.0.x --> >=2.0.0 <3.0.0
// ^1.2, ^1.2.x --> >=1.2.0 <2.0.0
// ^1.2.3 --> >=1.2.3 <2.0.0
// ^1.2.0 --> >=1.2.0 <2.0.0
var constraintCaretRegex *regexp.Regexp

func rewriteCarets(i string) string {
	m := constraintCaretRegex.FindAllStringSubmatch(i, -1)
	if m == nil {
		return i
	}
	o := i
	for _, v := range m {
		if isX(v[1]) {
			o = strings.Replace(o, v[0], ">=0.0.0", 1)
		} else if isX(strings.TrimPrefix(v[2], ".")) {
			ii, err := strconv.ParseInt(v[1], 10, 32)

			// The regular expression and isX checking should already make this
			// safe so something is broken in the lib.
			if err != nil {
				panic("Error converting string to Int. Should not occur.")
			}
			t := fmt.Sprintf(">= %s.0%s, < %d", v[1], v[4], ii+1)
			o = strings.Replace(o, v[0], t, 1)
		} else if isX(strings.TrimPrefix(v[3], ".")) {
			ii, err := strconv.ParseInt(v[1], 10, 32)

			// The regular expression and isX checking should already make this
			// safe so something is broken in the lib.
			if err != nil {
				panic("Error converting string to Int. Should not occur.")
			}
			t := fmt.Sprintf(">= %s%s.0%s, < %d", v[1], v[2], v[4], ii+1)
			o = strings.Replace(o, v[0], t, 1)
		} else {
			ii, err := strconv.ParseInt(v[1], 10, 32)
			// The regular expression and isX checking should already make this
			// safe so something is broken in the lib.
			if err != nil {
				panic("Error converting string to Int. Should not occur.")
			}

			t := fmt.Sprintf(">= %s%s%s%s, < %d", v[1], v[2], v[3], v[4], ii+1)
			o = strings.Replace(o, v[0], t, 1)
		}
	}

	return o
}