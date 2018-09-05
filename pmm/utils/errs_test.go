package utils

import "fmt"

func ExampleErrs() {
	var errs Errs
	errs = append(errs, fmt.Errorf("problem one"))
	errs = append(errs, fmt.Errorf("problem two"))

	fmt.Printf("there was an error:%s", errs)
	// Output: there was an error:
	//* problem one
	//* problem two
}
