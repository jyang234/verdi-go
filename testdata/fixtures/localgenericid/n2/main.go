// Witness n2: promoted-method wrappers over function-local receivers. Two
// same-named function-local `result` types embed different named types, so
// go/ssa builds two DISTINCT promotion wrappers that share one display FQN
// ((*main.result).QueryContext, and the value-receiver form main.result).
//
// These carry no type arguments, so InstanceDiscriminator used to return "" for
// them; and they carry a receiver, which mergeKey deliberately excludes from its
// dedup subset. The input fell between the two mechanisms and reached finalize
// with an empty discriminator on both sides. Both the value-receiver and the
// pointer-receiver wrapper form are exercised here.
package main

import "fmt"

type ifc interface{ QueryContext() string }

type embA struct{}

func (embA) QueryContext() string { return "A" }

type embB struct{}

func (embB) QueryContext() string { return "B" }

func firstPointer() ifc {
	type result struct{ embA }
	return &result{}
}

func secondPointer() ifc {
	type result struct{ embB }
	return &result{}
}

func firstValue() ifc {
	type result struct{ embA }
	return result{}
}

func secondValue() ifc {
	type result struct{ embB }
	return result{}
}

func main() {
	fmt.Println(
		firstPointer().QueryContext(),
		secondPointer().QueryContext(),
		firstValue().QueryContext(),
		secondValue().QueryContext(),
	)
}
