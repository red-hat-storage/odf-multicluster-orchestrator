package utils

import (
	"fmt"

	"github.com/blang/semver/v4"
)

type operator string

var (
	Eq operator = "eq"
	Lt operator = "lt"
	Gt operator = "gt"
)

func CompareSemverMajorMinorVersions(v1, v2 string, opr operator) (bool, error) {
	v1Sem, err := semver.Parse(v1)
	if err != nil {
		return false, err
	}
	v1Sem.Pre = nil
	v1Sem.Build = nil

	v2Sem, err := semver.Parse(v2)
	if err != nil {
		return false, err
	}
	v2Sem.Pre = nil
	v2Sem.Build = nil

	switch opr {
	case Eq:
		return v1Sem.Equals(v2Sem), nil
	case Lt:
		return v1Sem.LT(v2Sem), nil
	case Gt:
		return v1Sem.GT(v2Sem), nil
	default:
		return false, fmt.Errorf("unknown operator %q", opr)
	}
}
