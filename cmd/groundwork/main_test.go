package main

import "testing"

func TestRunSmoke(t *testing.T) {
	cases := [][]string{
		{"version"},
		{"help"},
		{"policy-check", "../../testdata/groundwork/policies/layeredsvc.json"},
		{"reach", "../../testdata/groundwork/goldens/layeredsvc.graph.json",
			"(*example.com/layeredsvc/internal/handler.Server).UpdateUser"},
		// fitness passes on both fixtures (layeredsvc cleanly, blindsvc with a
		// caution that does not fail the gate).
		{"fitness", "../../testdata/groundwork/policies/layeredsvc.json",
			"../../testdata/groundwork/goldens/layeredsvc.graph.json"},
		{"fitness", "../../testdata/groundwork/policies/blindsvc.json",
			"../../testdata/groundwork/goldens/blindsvc.graph.json"},
	}
	for _, args := range cases {
		if err := run(args); err != nil {
			t.Errorf("run(%v) = %v, want nil", args, err)
		}
	}
}

func TestRunErrors(t *testing.T) {
	cases := [][]string{
		{"bogus"},
		{"reach", "../../testdata/groundwork/goldens/layeredsvc.graph.json", "no.Such.Func"},
		{"reach", "/nonexistent/graph.json", "x"},
		{"policy-check", "/nonexistent/policy.json"},
		{"fitness", "/nonexistent/policy.json", "../../testdata/groundwork/goldens/layeredsvc.graph.json"},
		{"fitness", "../../testdata/groundwork/policies/layeredsvc.json", "/nonexistent/graph.json"},
	}
	for _, args := range cases {
		if err := run(args); err == nil {
			t.Errorf("run(%v) = nil, want error", args)
		}
	}
}
