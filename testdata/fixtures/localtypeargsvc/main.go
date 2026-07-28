package main

import "fmt"

func report[T any](v T) string {
	return fmt.Sprintf("%v", v)
}

func alpha() string { type result struct{ Value int }; return report(result{Value: 1}) }
func beta() string  { type result struct{ Value int }; return report(result{Value: 2}) }
func gamma() string { type result struct{ Text string }; return report(result{Text: "three"}) }

func main() {
	fmt.Println(alpha(), beta(), gamma())
}
