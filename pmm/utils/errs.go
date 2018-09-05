package utils

import (
	"bytes"
	"fmt"
)

// Errs is a slice of errors implementing the error interface.
type Errs []error

func (errs Errs) Error() string {
	if len(errs) == 0 {
		return ""
	}
	buf := &bytes.Buffer{}
	for _, err := range errs {
		fmt.Fprintf(buf, "\n* %s", err)
	}
	return buf.String()
}
